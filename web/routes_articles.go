package web

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/delaneyj/datastar"
	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"zombiezen.com/go/sqlite"
)

type CommentData struct {
	ID                int64
	Body              string
	At                time.Time
	CommenterId       int64
	CommenterUsername string
	CommenterImageURL string
}

func setupArticlesRoutes(r chi.Router, db *toolbelt.Database) {
	r.Route("/articles", func(articlesRouter chi.Router) {

		articlesRouter.Route("/new", func(editorRouter chi.Router) {
			editorRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				u, _ := UserFromContext(ctx)

				a := &ArticleEditData{}
				PageArticleUpsert(r, u, a).Render(r.Context(), w)
			})

			editorRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
				a := &ArticleEditData{}
				if err := datastar.BodyUnmarshal(r, a); err != nil {
					http.Error(w, "failed to parse request body", http.StatusBadRequest)
					return
				}

				ctx := r.Context()
				u, _ := UserFromContext(ctx)

				if u == nil {
					http.Error(w, "user required", http.StatusUnauthorized)
					return
				}

				sse := datastar.NewSSE(w, r)

				a.Title = strings.TrimSpace(a.Title)
				a.Description = strings.TrimSpace(a.Description)
				a.Body = strings.TrimSpace(a.Body)

				var validationErrors []error
				if a.Title == "" {
					validationErrors = append(validationErrors, fmt.Errorf("title required"))
				}

				if a.Description == "" {
					validationErrors = append(validationErrors, fmt.Errorf("description required"))
				}

				if a.Body == "" {
					validationErrors = append(validationErrors, fmt.Errorf("body required"))
				}

				tagParts := strings.Split(a.NewTags, " ")
				possibleTagNames := make(map[string]struct{}, len(tagParts))
				for _, part := range tagParts {
					possibleTagName := strings.TrimSpace(part)
					if possibleTagName == "" {
						continue
					}
					possibleTagNames[possibleTagName] = struct{}{}
				}
				tags := make([]*zz.TagModel, 0, len(possibleTagNames))

				if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
					tagByNameStmt := zz.TagByName(tx)

					for possibleTagName := range possibleTagNames {
						res, err := tagByNameStmt.Run(possibleTagName)
						if err != nil {
							return fmt.Errorf("failed to get tag by name: %w", err)
						}

						tag := &zz.TagModel{}
						if res != nil {
							tag.Id = res.Id
							tag.Name = res.Name
						} else {
							tag.Id = toolbelt.NextID()
							tag.Name = possibleTagName

							if err := zz.CreateTag(tx).Run(tag); err != nil {
								return fmt.Errorf("failed to create tag: %w", err)
							}
						}

						tags = append(tags, tag)
					}

					return nil
				}); err != nil {
					http.Error(w, "failed to create tags", http.StatusInternalServerError)
					return
				}
				if len(tags) > 0 {
					slices.SortFunc(tags, func(a, b *zz.TagModel) int {
						return strings.Compare(a.Name, b.Name)
					})

					a.NewTags = ""
					datastar.RenderFragmentTempl(sse, articleEditor(r, a, tags...))
				}

				if len(validationErrors) > 0 {
					datastar.RenderFragmentTempl(sse, errorMessages(validationErrors...))
					return
				}

				// Create article
				articleID := toolbelt.NextID()
				if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
					if err := zz.CreateArticle(tx).Run(&zz.ArticleModel{
						Id:          articleID,
						AuthorId:    u.Id,
						Title:       a.Title,
						Slug:        toolbelt.Kebab(a.Title),
						Description: a.Description,
						Body:        a.Body,
					}); err != nil {
						return fmt.Errorf("failed to create article: %w", err)
					}

					for _, tag := range tags {
						if err := zz.CreateArticleTag(tx).Run(&zz.ArticleTagModel{
							Id:        toolbelt.NextID(),
							ArticleId: articleID,
							TagId:     tag.Id,
						}); err != nil {
							return fmt.Errorf("failed to create article tag: %w", err)
						}
					}

					return nil
				}); err != nil {
					datastar.RenderFragmentTempl(sse, errorMessages(
						fmt.Errorf("failed to create article %w", err),
					))
				} else {
					datastar.Redirect(sse, fmt.Sprintf("/articles/%d", articleID))
				}
			})
		})

		articlesRouter.Route("/{articleId}", func(articleRouter chi.Router) {
			articleRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				u, _ := UserFromContext(ctx)

				articleIDRaw := chi.URLParam(r, "articleId")
				articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
				if err != nil {
					http.Error(w, "invalid article ID", http.StatusBadRequest)
					return
				}

				var (
					author                   *zz.UserModel
					article                  *zz.ArticleModel
					favoriteCount            int64
					comments                 []CommentData
					isFollowing, isFavorited bool
				)
				if err := db.ReadTX(ctx, func(tx *sqlite.Conn) error {
					article, err = zz.OnceReadByIDArticle(tx, articleID)
					if err != nil {
						return fmt.Errorf("failed to get article: %w", err)
					}

					author, err = zz.OnceReadByIDUser(tx, article.AuthorId)
					if err != nil {
						return fmt.Errorf("failed to get author: %w", err)
					}

					favoriteCount, err = zz.OnceArticleFavoriteCount(tx, articleID)
					if err != nil {
						return fmt.Errorf("failed to get favorite count: %w", err)
					}

					commentsRaw, err := zz.OnceArticleComments(tx, articleID)
					if err != nil {
						return fmt.Errorf("failed to get comments: %w", err)
					}

					comments = make([]CommentData, len(commentsRaw))
					for i, c := range commentsRaw {
						comments[i] = CommentData{
							ID:                c.CommentId,
							Body:              c.Body,
							At:                c.CreatedAt,
							CommenterId:       c.CommenterId,
							CommenterUsername: c.CommenterName,
							CommenterImageURL: c.CommenterImage,
						}
					}

					if u != nil {
						isFollowing, err = zz.OnceIsUserFollowing(tx, zz.IsUserFollowingParams{
							UserId:    u.Id,
							FollowsId: author.Id,
						})
						if err != nil {
							return fmt.Errorf("failed to check if user is following: %w", err)
						}

						isFavorited, err = zz.OnceHasUserFavorited(tx, zz.HasUserFavoritedParams{
							UserId:    u.Id,
							ArticleId: articleID,
						})
						if err != nil {
							return fmt.Errorf("failed to check if article is favorited: %w", err)
						}
					}

					return nil
				}); err != nil {
					http.Error(w, "failed to get article", http.StatusInternalServerError)
					return
				}

				if article == nil {
					http.Error(w, "article not found", http.StatusNotFound)
					return
				}

				PageArticle(
					r, u, author, article, favoriteCount,
					isFollowing, isFavorited, comments...,
				).Render(r.Context(), w)
			})

			articleRouter.Delete("/", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				u, _ := UserFromContext(ctx)

				if u == nil {
					http.Error(w, "user required", http.StatusUnauthorized)
					return
				}

				articleIDRaw := chi.URLParam(r, "articleId")
				articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
				if err != nil {
					http.Error(w, "invalid article ID", http.StatusBadRequest)
					return
				}

				if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
					article, err := zz.OnceReadByIDArticle(tx, articleID)
					if err != nil {
						return fmt.Errorf("failed to get article: %w", err)
					}

					if article.AuthorId != u.Id {
						return fmt.Errorf("user is not author")
					}

					if err := zz.DeleteArticle(tx).Run(articleID); err != nil {
						return fmt.Errorf("failed to delete article: %w", err)
					}

					return nil
				}); err != nil {
					http.Error(w, "failed to delete article", http.StatusInternalServerError)
					return
				}

				sse := datastar.NewSSE(w, r)
				datastar.Redirect(sse, "/")
			})

			articleRouter.Route("/edit", func(editRouter chi.Router) {
				editRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
					ctx := r.Context()
					u, _ := UserFromContext(ctx)

					articleIDRaw := chi.URLParam(r, "articleId")
					articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
					if err != nil {
						http.Error(w, "invalid article ID", http.StatusBadRequest)
						return
					}

					var (
						article *zz.ArticleModel
						tags    []*zz.TagModel
					)
					articleEditData := &ArticleEditData{}

					isUserAuthor := false
					if err := db.ReadTX(ctx, func(tx *sqlite.Conn) error {
						article, err = zz.OnceReadByIDArticle(tx, articleID)
						if err != nil {
							return fmt.Errorf("failed to get article: %w", err)
						}

						if article.AuthorId == u.Id {
							isUserAuthor = true
						} else {
							return nil
						}

						articleEditData.Title = article.Title
						articleEditData.Description = article.Description
						articleEditData.Body = article.Body

						res, err := zz.OnceTagsForArticle(tx, articleID)
						if err != nil {
							return fmt.Errorf("failed to get tags: %w", err)
						}
						for _, row := range res {
							tags = append(tags, &zz.TagModel{
								Id:   row.Id,
								Name: row.Name,
							})
						}

						return nil
					}); err != nil {
						http.Error(w, "failed to get article", http.StatusInternalServerError)
						return
					}

					if !isUserAuthor || article == nil {
						http.Redirect(w, r, "/", http.StatusSeeOther)
						return
					}

					PageArticleUpsert(r, u, articleEditData, tags...).Render(r.Context(), w)
				})

				editRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
					a := &ArticleEditData{}
					if err := datastar.BodyUnmarshal(r, a); err != nil {
						http.Error(w, "failed to parse request body", http.StatusBadRequest)
						return
					}

					ctx := r.Context()
					u, _ := UserFromContext(ctx)

					if u == nil {
						http.Error(w, "user required", http.StatusUnauthorized)
						return
					}

					sse := datastar.NewSSE(w, r)

					a.Title = strings.TrimSpace(a.Title)
					a.Description = strings.TrimSpace(a.Description)
					a.Body = strings.TrimSpace(a.Body)

					var validationErrors []error
					if a.Title == "" {
						validationErrors = append(validationErrors, fmt.Errorf("title required"))
					}

					if a.Description == "" {
						validationErrors = append(validationErrors, fmt.Errorf("description required"))
					}

					if a.Body == "" {
						validationErrors = append(validationErrors, fmt.Errorf("body required"))
					}

					tagParts := strings.Split(a.NewTags, " ")
					possibleTagNames := make(map[string]struct{}, len(tagParts))
					for _, part := range tagParts {
						possibleTagName := strings.TrimSpace(part)
						if possibleTagName == "" {
							continue
						}
						possibleTagNames[possibleTagName] = struct{}{}
					}
					tags := make([]*zz.TagModel, 0, len(possibleTagNames))

					if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
						tagByNameStmt := zz.TagByName(tx)

						for possibleTagName := range possibleTagNames {
							res, err := tagByNameStmt.Run(possibleTagName)
							if err != nil {
								return fmt.Errorf("failed to get tag by name: %w", err)
							}

							tag := &zz.TagModel{}
							if res != nil {
								tag.Id = res.Id
								tag.Name = res.Name
							} else {
								tag.Id = toolbelt.NextID()
								tag.Name = possibleTagName

								if err := zz.CreateTag(tx).Run(tag); err != nil {
									return fmt.Errorf("failed to create tag: %w", err)
								}
							}

							tags = append(tags, tag)
						}

						return nil
					}); err != nil {
						http.Error(w, "failed to create tags", http.StatusInternalServerError)
						return
					}
					if len(tags) > 0 {
						slices.SortFunc(tags, func(a, b *zz.TagModel) int {
							return strings.Compare(a.Name, b.Name)
						})
					}

					if len(validationErrors) > 0 {
						datastar.RenderFragmentTempl(sse, errorMessages(validationErrors...))
						return
					}

					articleIDRaw := chi.URLParam(r, "articleId")
					articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
					if err != nil {
						http.Error(w, "invalid article ID", http.StatusBadRequest)
						return
					}

					if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
						article, err := zz.OnceReadByIDArticle(tx, articleID)
						if err != nil {
							return fmt.Errorf("failed to get article: %w", err)
						}

						if article.AuthorId != u.Id {
							return fmt.Errorf("user is not author")
						}

						article.Title = a.Title
						article.Slug = toolbelt.Kebab(a.Title)
						article.Description = a.Description
						article.Body = a.Body

						if err := zz.UpdateArticle(tx).Run(article); err != nil {
							return fmt.Errorf("failed to update article: %w", err)
						}

						for _, tag := range tags {
							if err := zz.CreateArticleTag(tx).Run(&zz.ArticleTagModel{
								Id:        toolbelt.NextID(),
								ArticleId: articleID,
								TagId:     tag.Id,
							}); err != nil {
								// Unique constraint violation is fine, just skip
								continue
							}
						}

						return nil
					}); err != nil {
						datastar.RenderFragmentTempl(sse, errorMessages(
							fmt.Errorf("failed to update article %w", err),
						))
					} else {
						datastar.Redirect(sse, fmt.Sprintf("/articles/%d", articleID))
					}
				})

				editRouter.Delete("/tags/{tagId}", func(w http.ResponseWriter, r *http.Request) {
					ctx := r.Context()
					u, _ := UserFromContext(ctx)

					if u == nil {
						http.Error(w, "user required", http.StatusUnauthorized)
						return
					}

					articleIDRaw := chi.URLParam(r, "articleId")
					articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
					if err != nil {
						http.Error(w, "invalid article ID", http.StatusBadRequest)
						return
					}

					tagIDRaw := chi.URLParam(r, "tagId")
					tagID, err := strconv.ParseInt(tagIDRaw, 10, 64)
					if err != nil {
						http.Error(w, "invalid tag ID", http.StatusBadRequest)
						return
					}

					sse := datastar.NewSSE(w, r)

					if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
						article, err := zz.OnceReadByIDArticle(tx, articleID)
						if err != nil {
							return fmt.Errorf("failed to get article: %w", err)
						}

						if article.AuthorId != u.Id {
							return fmt.Errorf("user is not author")
						}

						if err := zz.OnceDeleteTagFromArticle(tx, zz.DeleteTagFromArticleParams{
							ArticleId: articleID,
							TagId:     tagID,
						}); err != nil {
							return fmt.Errorf("failed to delete tag: %w", err)
						}

						return nil
					}); err != nil {
						datastar.RenderFragmentTempl(sse, errorMessages(
							fmt.Errorf("failed to delete tag %w", err),
						))
					}

					datastar.Redirect(sse, fmt.Sprintf("/articles/%d/edit", articleID))
				})
			})

			articleRouter.Route("/favorite", func(favoriteRouter chi.Router) {
				favoriteRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
					ctx := r.Context()
					me, _ := UserFromContext(ctx)

					if me == nil {
						http.Error(w, "user required", http.StatusUnauthorized)
						return
					}

					articleIDRaw := chi.URLParam(r, "articleId")
					articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
					if err != nil {
						http.Error(w, "invalid article ID", http.StatusBadRequest)
						return
					}

					if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
						alreadyFavorited, err := zz.OnceHasUserFavorited(tx, zz.HasUserFavoritedParams{
							UserId:    me.Id,
							ArticleId: articleID,
						})
						if err != nil {
							return fmt.Errorf("failed to check if article is already favorited: %w", err)
						}

						if alreadyFavorited {
							return fmt.Errorf("article already favorited")
						}

						if err := zz.OnceCreateArticleFavorite(tx, &zz.ArticleFavoriteModel{
							Id:        toolbelt.NextID(),
							UserId:    me.Id,
							ArticleId: articleID,
						}); err != nil {
							return fmt.Errorf("failed to favorite article: %w", err)
						}
						return nil
					}); err != nil {
						http.Error(w, "failed to favorite article", http.StatusInternalServerError)
						return
					}

					sse := datastar.NewSSE(w, r)

					from := r.URL.Query().Get("from")
					if from != "" {
						datastar.Redirect(sse, from)
					}
				})

				favoriteRouter.Delete("/", func(w http.ResponseWriter, r *http.Request) {
					ctx := r.Context()
					me, _ := UserFromContext(ctx)

					if me == nil {
						http.Error(w, "user required", http.StatusUnauthorized)
						return
					}

					articleIDRaw := chi.URLParam(r, "articleId")
					articleID, err := strconv.ParseInt(articleIDRaw, 10, 64)
					if err != nil {
						http.Error(w, "invalid article ID", http.StatusBadRequest)
						return
					}

					if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
						if err := zz.OnceDeleteFavoritedArticle(tx, zz.DeleteFavoritedArticleParams{
							UserId:    me.Id,
							ArticleId: articleID,
						}); err != nil {
							return fmt.Errorf("failed to unfavorite article: %w", err)
						}
						return nil
					}); err != nil {
						http.Error(w, "failed to unfavorite article", http.StatusInternalServerError)
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
	})
}
