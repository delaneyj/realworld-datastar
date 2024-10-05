package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/delaneyj/datastar"
	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"zombiezen.com/go/sqlite"
)

func setupAuthRoutes(r chi.Router, db *toolbelt.Database, sessionStore sessions.Store) {

	r.Route("/auth", func(authRouter chi.Router) {
		authRouter.Post("/logout", func(w http.ResponseWriter, r *http.Request) {
			sess, err := sessionStore.Get(r, "conduit")
			if err != nil {
				http.Error(w, "failed to get session", http.StatusInternalServerError)
				return
			}

			delete(sess.Values, "userID")
			if err := sess.Save(r, w); err != nil {
				http.Error(w, "failed to save session", http.StatusInternalServerError)
				return
			}

			sse := datastar.NewSSE(w, r)
			datastar.Redirect(sse, "/auth/login")
		})

		authRouter.Route("/login", func(loginRouter chi.Router) {
			loginRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				if _, ok := UserFromContext(r.Context()); ok {
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}

				PageAuthenticationLogin(r, nil).Render(r.Context(), w)
			})

			loginRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
				type Form struct {
					Email    string `json:"email"`
					Password string `json:"password"`
				}

				form := &Form{}
				if err := datastar.BodyUnmarshal(r, form); err != nil {
					http.Error(w, "failed to parse request body", http.StatusBadRequest)
					return
				}

				var res *zz.UserByEmailRes
				err := db.ReadTX(r.Context(), func(tx *sqlite.Conn) (err error) {
					res, err = zz.OnceUserByEmail(tx, form.Email)
					if err != nil {
						return fmt.Errorf("failed to get user by email: %w", err)
					}
					return nil
				})
				if err != nil {
					http.Error(w, "failed to get user by email", http.StatusInternalServerError)
					return
				}

				if res == nil {
					err = errors.New("user with email not found")
				} else if res.Password != form.Password {
					err = errors.New("incorrect password")
				}

				if err == nil {
					sess, err := sessionStore.Get(r, "conduit")
					if err != nil {
						http.Error(w, "failed to get session", http.StatusInternalServerError)
						return
					}

					sess.Values["userID"] = res.Id
					if err := sess.Save(r, w); err != nil {
						http.Error(w, "failed to save session", http.StatusInternalServerError)
						return
					}
				}

				sse := datastar.NewSSE(w, r)
				if err != nil {
					datastar.RenderFragmentTempl(sse, errorMessages(err))
					return
				}

				datastar.Redirect(sse, "/")
			})
		})

		authRouter.Get("/register", func(w http.ResponseWriter, r *http.Request) {
			PageAuthenticationRegister(r, nil).Render(r.Context(), w)
		})
	})
}
