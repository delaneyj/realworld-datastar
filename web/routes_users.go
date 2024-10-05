package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/delaneyj/datastar"
	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"zombiezen.com/go/sqlite"
)

func setupUsersRoutes(r chi.Router, db *toolbelt.Database) {
	r.Route("/users/{userID}", func(userRouter chi.Router) {
		userRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			me, _ := UserFromContext(ctx)

			userIDRaw := chi.URLParam(r, "userID")
			userID, err := strconv.ParseInt(userIDRaw, 10, 64)
			if err != nil {
				http.Error(w, "invalid user ID", http.StatusBadRequest)
				return
			}

			// get feed query params
			feed := r.URL.Query().Get("feed")
			if feed == "" {
				q := r.URL.Query()
				q.Add("feed", "my")
				r.URL.RawQuery = q.Encode()
				http.Redirect(w, r, r.URL.String(), http.StatusSeeOther)
			}

			offsetRaw := r.URL.Query().Get("offset")
			if offsetRaw == "" {
				offsetRaw = "0"
			}
			offset, err := strconv.ParseInt(offsetRaw, 10, 64)
			if err != nil {
				http.Error(w, "invalid offset", http.StatusBadRequest)
				return
			}

			feedData := &FeedData{
				Names:   []string{"my", "favorited"},
				Current: feed,
				Limit:   3,
				Offset:  offset,
			}

			validFeedName := false
			for _, name := range feedData.Names {
				if feed == name {
					validFeedName = true
					break
				}
			}
			if !validFeedName {
				http.Error(w, "invalid feed name", http.StatusBadRequest)
				return
			}

			var (
				u           *zz.UserModel
				isFollowing bool
			)
			if err := db.ReadTX(ctx, func(tx *sqlite.Conn) (err error) {
				u, err = zz.OnceReadByIDUser(tx, userID)
				if err != nil {
					return fmt.Errorf("failed to get user by ID: %w", err)
				}

				if me != nil {
					isFollowing, err = zz.OnceIsUserFollowing(tx, zz.IsUserFollowingParams{
						UserId:    me.Id,
						FollowsId: userID,
					})
					if err != nil {
						return fmt.Errorf("failed to check if user is following: %w", err)
					}
				}

				articleTagsStmt := zz.TagsForArticle(tx)
				favoriteCountStmt := zz.ArticleFavoriteCount(tx)

				switch feed {
				case "my":
					res, err := zz.OnceArticlePreviewsByAuthor(tx, zz.ArticlePreviewsByAuthorParams{
						AuthorId: userID,
						Limit:    feedData.Limit,
						Offset:   feedData.Offset,
					})
					if err != nil {
						return fmt.Errorf("failed to get articles by author: %w", err)
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

					feedData.TotalArticles, err = zz.OnceArticleCountByAuthor(tx, userID)
					if err != nil {
						return fmt.Errorf("failed to get total articles by author: %w", err)
					}

				case "favorited":
					res, err := zz.OnceArticlePreviewsByFavoriter(tx, zz.ArticlePreviewsByFavoriterParams{
						FavoriterId: userID,
						Limit:       feedData.Limit,
						Offset:      feedData.Offset,
					})
					if err != nil {
						return fmt.Errorf("failed to get articles by favoriter: %w", err)
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

					feedData.TotalArticles, err = zz.OnceArticleCountByFavoriter(tx, userID)
					if err != nil {
						return fmt.Errorf("failed to get total articles by favoriter: %w", err)
					}
				}

				for _, preview := range feedData.Articles {
					res, err := articleTagsStmt.Run(preview.ArticleId)
					if err != nil {
						return fmt.Errorf("failed to get tags for article: %w", err)
					}
					for _, row := range res {
						preview.Tags = append(preview.Tags, &zz.TagModel{
							Id:   row.Id,
							Name: row.Name,
						})
					}

					preview.FavoriteCount, err = favoriteCountStmt.Run(preview.ArticleId)
					if err != nil {
						return fmt.Errorf("failed to get favorite count for article: %w", err)
					}

				}

				return nil
			}); err != nil {
				http.Error(w, "failed to get user", http.StatusInternalServerError)
				return
			}

			PageUser(r, me, u, isFollowing, feedData).Render(r.Context(), w)
		})

		userRouter.Route("/follow", func(followRouter chi.Router) {
			followRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				me, _ := UserFromContext(ctx)

				if me == nil {
					http.Error(w, "user required", http.StatusUnauthorized)
					return
				}

				userIDRaw := chi.URLParam(r, "userID")
				userID, err := strconv.ParseInt(userIDRaw, 10, 64)
				if err != nil {
					http.Error(w, "invalid user ID", http.StatusBadRequest)
					return
				}

				if me == nil {
					http.Error(w, "user required", http.StatusUnauthorized)
					return
				}

				if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
					if err := zz.OnceCreateFollowing(tx, &zz.FollowingModel{
						Id:        toolbelt.NextID(),
						UserId:    me.Id,
						FollowsId: userID,
					}); err != nil {
						return fmt.Errorf("failed to follow user: %w", err)
					}
					return nil
				}); err != nil {
					http.Error(w, "failed to follow user", http.StatusInternalServerError)
					return
				}

				sse := datastar.NewSSE(w, r)

				from := r.URL.Query().Get("from")
				if from != "" {
					datastar.Redirect(sse, from)
				}
			})

			followRouter.Delete("/", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				me, _ := UserFromContext(ctx)

				if me == nil {
					http.Error(w, "user required", http.StatusUnauthorized)
					return
				}

				userIDRaw := chi.URLParam(r, "userID")
				userID, err := strconv.ParseInt(userIDRaw, 10, 64)
				if err != nil {
					http.Error(w, "invalid user ID", http.StatusBadRequest)
					return
				}

				if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
					if err := zz.OnceDeleteFollow(tx, zz.DeleteFollowParams{
						UserId:    me.Id,
						FollowsId: userID,
					}); err != nil {
						return fmt.Errorf("failed to unfollow user: %w", err)
					}
					return nil
				}); err != nil {
					http.Error(w, "failed to unfollow user", http.StatusInternalServerError)
					return
				}

				sse := datastar.NewSSE(w, r)

				from := r.URL.Query().Get("from")
				if from != "" {
					datastar.Redirect(sse, from)
				}
			})
		})
	})
}
