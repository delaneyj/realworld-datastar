package web

import (
	"fmt"
	"net/http"
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

		feed := &FeedData{
			Names:   []string{"your", "global"},
			Current: "global",
			Limit:   10,
			Offset:  0,
		}

		if u != nil {
			if err := db.ReadTX(ctx, func(tx *sqlite.Conn) error {
				res, err := zz.OnceYourFeedArticlePreviews(tx, zz.YourFeedArticlePreviewsParams{
					UserId: u.Id,
					Offset: int64(feed.Offset),
					Limit:  int64(feed.Limit),
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

					preview.FavoriteCount, err = zz.OnceArticleFavoriteCount(tx, row.ArticleId)
					if err != nil {
						return fmt.Errorf("failed to get favorite count: %w", err)
					}

					res, err := zz.OnceTagsForArticle(tx, row.ArticleId)
					if err != nil {
						return fmt.Errorf("failed to get tags for article: %w", err)
					}
					for _, row := range res {
						preview.Tags = append(preview.Tags, &zz.TagModel{
							Id:   row.Id,
							Name: row.Name,
						})
					}
					feed.Articles = append(feed.Articles, preview)
				}

				topTagRes, err := zz.OnceTopTags(tx, 10)
				if err != nil {
					return fmt.Errorf("failed to get top tags: %w", err)
				}
				for _, row := range topTagRes {
					feed.PopularTags = append(feed.PopularTags, row.Name)
				}

				return nil
			}); err != nil {
				http.Error(w, "failed to read from database", http.StatusInternalServerError)
				return
			}
		}

		PageHome(r, u, feed).Render(r.Context(), w)
	})
}
