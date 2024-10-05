package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/delaneyj/datastar"
	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"zombiezen.com/go/sqlite"
)

func setupSettingsRoutes(r chi.Router, db *toolbelt.Database) {
	r.Route("/settings", func(settingsRouter chi.Router) {
		settingsRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			u, _ := UserFromContext(ctx)
			PageSettings(r, u).Render(ctx, w)
		})

		settingsRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			u, _ := UserFromContext(ctx)

			form := &zz.UserModel{}
			if err := datastar.BodyUnmarshal(r, form); err != nil {
				http.Error(w, "failed to parse request body", http.StatusBadRequest)
				return
			}

			sse := datastar.NewSSE(w, r)

			form.Username = strings.TrimSpace(form.Username)
			if form.Username == "" {
				datastar.RenderFragmentTempl(sse, errorMessages(errors.New("username is required")))
				return
			}

			u.Username = form.Username
			u.Email = form.Email
			u.Password = form.Password
			u.ImageUrl = form.ImageUrl
			u.Bio = form.Bio

			if err := db.WriteTX(ctx, func(tx *sqlite.Conn) error {
				if err := zz.OnceUpdateUser(tx, u); err != nil {
					return fmt.Errorf("failed to update user: %w", err)
				}
				return nil
			}); err != nil {
				http.Error(w, "failed to update user", http.StatusInternalServerError)
				return
			}

			datastar.Redirect(sse, "/")
		})
	})
}
