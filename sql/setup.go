package sql

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/jaswdr/faker/v2"
	"golang.org/x/crypto/bcrypt"
	"zombiezen.com/go/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func SetupDB(ctx context.Context, dataFolder string, shouldClear bool) (*toolbelt.Database, error) {
	migrationsDir := "migrations"
	migrationsFiles, err := migrationsFS.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}
	slices.SortFunc(migrationsFiles, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	migrations := make([]string, len(migrationsFiles))
	for i, file := range migrationsFiles {
		fn := filepath.Join(migrationsDir, file.Name())
		f, err := migrationsFS.Open(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to open migration file: %w", err)
		}
		defer f.Close()

		content, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file: %w", err)
		}

		migrations[i] = string(content)
	}

	dbFolder := filepath.Join(dataFolder, "database")
	if shouldClear {
		log.Printf("Clearing database folder: %s", dbFolder)
		if err := os.RemoveAll(dbFolder); err != nil {
			return nil, fmt.Errorf("failed to remove database folder: %w", err)
		}
	}
	dbFilename := filepath.Join(dbFolder, "conduit.sqlite")
	db, err := toolbelt.NewDatabase(ctx, dbFilename, migrations)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	if err := SeedDBIfEmpty(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to seed database: %w", err)
	}

	return db, nil
}

func SeedDBIfEmpty(ctx context.Context, db *toolbelt.Database) error {
	isEmpty := true
	if err := db.ReadTX(ctx, func(tx *sqlite.Conn) error {
		count, err := zz.OnceCountUsers(tx)
		if err != nil {
			return fmt.Errorf("failed to count users: %w", err)
		}
		isEmpty = count == 0
		return nil
	}); err != nil {
		return fmt.Errorf("failed to check if database is empty: %w", err)
	}

	if !isEmpty {
		return nil
	}

	now := time.Now()
	randSource := rand.NewSource(0)
	r := rand.New(randSource)
	fake := faker.NewWithSeed(randSource)

	if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
		userIds := make([]int64, 64)
		createUserStmt := zz.CreateUser(tx)
		userIds[0] = 1

		passwordHash, err := bcrypt.GenerateFromPassword([]byte("correctHorseBatteryStapler"), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		if err := createUserStmt.Run(&zz.UserModel{
			Id:           1,
			Username:     "admin",
			Email:        "admin@example.com",
			PasswordHash: passwordHash,
			Bio:          "Admin user",
			ImageUrl:     "https://i.pravatar.cc/150?u=1",
		}); err != nil {
			return fmt.Errorf("failed to create admin user: %w", err)
		}

		for i := 1; i < len(userIds); i++ {
			userID := toolbelt.NextID()
			password := fmt.Sprintf("%d", userID)
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}

			if err := createUserStmt.Run(&zz.UserModel{
				Id:           userID,
				Username:     fmt.Sprintf("%s%04d", fake.Internet().User(), i),
				Email:        fake.Internet().Email(),
				Bio:          fake.Lorem().Sentences(1)[0],
				ImageUrl:     fmt.Sprintf("https://i.pravatar.cc/150?u=%d", userID),
				PasswordHash: passwordHash,
			}); err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}
			userIds[i] = userID
		}

		// add user followers
		createFollowStmt := zz.CreateFollowing(tx)
		for _, userID := range userIds {
			for i := 0; i < r.Intn(10); i++ {
				followerID := toolbelt.RandSliceItem(r, userIds)
				if userID == followerID {
					continue
				}
				if err := createFollowStmt.Run(&zz.FollowingModel{
					Id:        toolbelt.NextID(),
					UserId:    userID,
					FollowsId: followerID,
				}); err != nil {
					// Unique constraint violation is fine, just skip
					continue
				}
			}
		}

		tagIDs := make([]int64, 20)
		createTagStmt := zz.CreateTag(tx)
		for i := range tagIDs {
			id := toolbelt.NextID()
			if err := createTagStmt.Run(&zz.TagModel{
				Id:   id,
				Name: fmt.Sprintf("%s%04d", fake.Lorem().Word(), i),
			}); err != nil {
				return fmt.Errorf("failed to create tag: %w", err)
			}
			tagIDs[i] = id
		}

		createArticleTagStmt := zz.CreateArticleTag(tx)
		createArticleStmt := zz.CreateArticle(tx)
		for i := 0; i < 500; i++ {
			articleID := toolbelt.NextID()
			if err := createArticleStmt.Run(&zz.ArticleModel{
				Id:          articleID,
				Slug:        fake.Lorem().Sentences(1)[0],
				Title:       fake.Lorem().Sentences(1)[0],
				Description: strings.Join(fake.Lorem().Sentences(2), "\n"),
				Body:        strings.Join(fake.Lorem().Sentences(5), "\n"),
				CreatedAt:   now,
				UpdatedAt:   now,
				AuthorId:    toolbelt.RandSliceItem(r, userIds),
			}); err != nil {
				return fmt.Errorf("failed to create article: %w", err)
			}

			for i := 0; i < r.Intn(10); i++ {
				if err := createArticleTagStmt.Run(&zz.ArticleTagModel{
					Id:        toolbelt.NextID(),
					ArticleId: articleID,
					TagId:     toolbelt.RandSliceItem(r, tagIDs),
				}); err != nil {
					// Unique constraint violation is fine, just skip
					continue
				}
			}

			favoriteArticleStmt := zz.CreateArticleFavorite(tx)
			for i := 0; i < r.Intn(20); i++ {
				if err := favoriteArticleStmt.Run(&zz.ArticleFavoriteModel{
					Id:        toolbelt.NextID(),
					ArticleId: articleID,
					UserId:    toolbelt.RandSliceItem(r, userIds),
				}); err != nil {
					// Unique constraint violation is fine, just skip
					continue
				}
			}

			commentStmt := zz.CreateComment(tx)
			for i := 0; i < r.Intn(10); i++ {
				if err := commentStmt.Run(&zz.CommentModel{
					Id:        toolbelt.NextID(),
					Body:      strings.Join(fake.Lorem().Sentences(2), "\n"),
					CreatedAt: now,
					UpdatedAt: now,
					ArticleId: articleID,
					AuthorId:  toolbelt.RandSliceItem(r, userIds),
				}); err != nil {
					return fmt.Errorf("failed to create comment: %w", err)
				}
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	return nil
}
