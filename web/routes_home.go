package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"zombiezen.com/go/sqlite"
)

type FeedData struct {
	Names         []string
	Current       string
	Limit, Offset int64
	Articles      []*ArticlePreview
	TotalArticles int64
	PopularTags   []string
}

type ArticlePreview struct {
	AuthorID      int64
	ArticleId     int64
	Username      string
	ImageUrl      string
	Title         string
	Description   string
	CreatedAt     time.Time
	Tags          []*zz.TagModel
	FavoriteCount int64
}

func setupHomeRoutes(r chi.Router, db *toolbelt.Database) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u, _ := UserFromContext(ctx)

		feedData := &FeedData{
			Names:  []string{"your", "global"},
			Limit:  3,
			Offset: 0,
		}

		feed := r.URL.Query().Get("feed")
		if feed == "" {
			feed = "your"
		}

		isValidFeedName := false
		for _, name := range feedData.Names {
			if feed == name {
				isValidFeedName = true
				break
			}
		}
		if !isValidFeedName {
			http.Error(w, "invalid feed name", http.StatusBadRequest)
			return
		}
		feedData.Current = feed

		offsetRaw := r.URL.Query().Get("offset")
		if offsetRaw == "" {
			offsetRaw = "0"
		}
		offset, err := strconv.ParseInt(offsetRaw, 10, 64)
		if err != nil {
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
		feedData.Offset = offset

		if u != nil {
			if err := db.ReadTX(ctx, func(tx *sqlite.Conn) (err error) {

				switch feedData.Current {
				case "your":
					res, err := zz.OnceYourFeedArticlePreviews(tx, zz.YourFeedArticlePreviewsParams{
						UserId: u.Id,
						Offset: feedData.Offset,
						Limit:  feedData.Limit,
					})
					if err != nil {
						return fmt.Errorf("failed to get your feed: %w", err)
					}

					for _, row := range res {
						preview := &ArticlePreview{
							ArticleId:   row.ArticleId,
							AuthorID:    row.AuthorId,
							Username:    row.Username,
							ImageUrl:    row.ImageUrl,
							Title:       row.Title,
							Description: row.Description,
						}
						feedData.Articles = append(feedData.Articles, preview)
					}

					feedData.TotalArticles, err = zz.OnceYourFeedArticleCount(tx, u.Id)
					if err != nil {
						return fmt.Errorf("failed to get your feed count: %w", err)
					}

				case "global":
					res, err := zz.OnceGlobalFeedArticlePreviews(tx, zz.GlobalFeedArticlePreviewsParams{
						Offset: feedData.Offset,
						Limit:  feedData.Limit,
					})
					if err != nil {
						return fmt.Errorf("failed to get global feed: %w", err)
					}

					for _, row := range res {
						preview := &ArticlePreview{
							ArticleId:   row.ArticleId,
							AuthorID:    row.AuthorId,
							Username:    row.Username,
							ImageUrl:    row.ImageUrl,
							Title:       row.Title,
							Description: row.Description,
						}
						feedData.Articles = append(feedData.Articles, preview)
					}

					feedData.TotalArticles, err = zz.OnceGlobalFeedArticleCount(tx)
					if err != nil {
						return fmt.Errorf("failed to get global feed count: %w", err)
					}
				}

				for _, preview := range feedData.Articles {
					preview.FavoriteCount, err = zz.OnceArticleFavoriteCount(tx, preview.ArticleId)
					if err != nil {
						return fmt.Errorf("failed to get favorite count: %w", err)
					}

					res, err := zz.OnceTagsForArticle(tx, preview.ArticleId)
					if err != nil {
						return fmt.Errorf("failed to get tags for article: %w", err)
					}
					for _, row := range res {
						preview.Tags = append(preview.Tags, &zz.TagModel{
							Id:   row.Id,
							Name: row.Name,
						})
					}
				}

				topTagRes, err := zz.OnceTopTags(tx, 10)
				if err != nil {
					return fmt.Errorf("failed to get top tags: %w", err)
				}
				for _, row := range topTagRes {
					feedData.PopularTags = append(feedData.PopularTags, row.Name)
				}

				return nil
			}); err != nil {
				http.Error(w, "failed to read from database", http.StatusInternalServerError)
				return
			}
		}

		PageHome(r, u, feedData).Render(r.Context(), w)
	})
}
