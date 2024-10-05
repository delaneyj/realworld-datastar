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
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
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
				} else {
					err = bcrypt.CompareHashAndPassword(res.PasswordHash, []byte(form.Password))
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

		authRouter.Route("/register", func(registerRouter chi.Router) {

			registerRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				if _, ok := UserFromContext(r.Context()); ok {
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}

				ctx := r.Context()
				form := &RegisterForm{}
				PageAuthenticationRegister(r, nil, form).Render(ctx, w)
			})

			registerRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
				if _, ok := UserFromContext(r.Context()); ok {
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}

				form := &RegisterForm{}
				if err := datastar.BodyUnmarshal(r, form); err != nil {
					http.Error(w, "failed to parse request body", http.StatusBadRequest)
					return
				}
				sse := datastar.NewSSE(w, r)

				form.Username = strings.TrimSpace(form.Username)
				form.Email = strings.TrimSpace(form.Email)

				validationErrors := []error{}
				appendAndSendValidationErrors := func(errs ...error) {
					validationErrors = append(validationErrors, errs...)
					ec := errorMessages(validationErrors...)
					datastar.RenderFragmentTempl(sse, ec)
				}

				if form.Username == "" {
					appendAndSendValidationErrors(errors.New("username is required"))

				}
				if form.Email == "" {
					appendAndSendValidationErrors(errors.New("email is required"))
				}
				if len(form.Password) < 8 {
					appendAndSendValidationErrors(errors.New("password must be at least 8 characters"))
				}

				var userID int64
				if len(validationErrors) == 0 {
					if err := db.WriteTX(r.Context(), func(tx *sqlite.Conn) error {
						emailUser, err := zz.OnceUserByEmail(tx, form.Email)
						if err != nil {
							return fmt.Errorf("failed to get user by email: %w", err)
						}
						if emailUser != nil {
							appendAndSendValidationErrors(errors.New("email is already in use"))
						}

						usernameUser, err := zz.OnceUserByUsername(tx, form.Username)
						if err != nil {
							return fmt.Errorf("failed to get user by username: %w", err)
						}
						if usernameUser != nil {
							appendAndSendValidationErrors(errors.New("username is already in use"))
						}

						if len(validationErrors) > 0 {
							return nil
						}

						passwordHash, err := bcrypt.GenerateFromPassword([]byte(form.Password), bcrypt.DefaultCost)
						if err != nil {
							return fmt.Errorf("failed to hash password: %w", err)
						}

						userID = toolbelt.NextID()
						user := &zz.UserModel{
							Id:           userID,
							Username:     form.Username,
							Email:        form.Email,
							PasswordHash: passwordHash,
							ImageUrl:     fmt.Sprintf("https://i.pravatar.cc/150?u=%d", userID),
						}

						if err := zz.OnceCreateUser(tx, user); err != nil {
							return fmt.Errorf("failed to create user: %w", err)
						}

						return nil
					}); err != nil {
						http.Error(w, "failed to create user", http.StatusInternalServerError)
						return
					}
				}

				if len(validationErrors) > 0 {
					return
				}

				datastar.Redirect(sse, "/auth/login")
			})
		})
	})
}
