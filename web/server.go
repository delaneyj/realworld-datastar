package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/delaneyj/realworld-datastar/sql/zz"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"zombiezen.com/go/sqlite"
)

type CtxKey string

const (
	CtxKeyUser CtxKey = "user"
)

func UserFromContext(ctx context.Context) (*zz.UserModel, bool) {
	userID, ok := ctx.Value(CtxKeyUser).(*zz.UserModel)
	return userID, ok
}

func ContextWithUser(ctx context.Context, user *zz.UserModel) context.Context {
	return context.WithValue(ctx, CtxKeyUser, user)
}

func RunHTTPServer(setupCtx context.Context, db *toolbelt.Database) error {
	sessionStore := sessions.NewCookieStore([]byte("conduit"))
	sessionStore.MaxAge(int(24 * time.Hour / time.Second))

	router := chi.NewRouter()
	router.Use(
		middleware.Logger,
		middleware.Recoverer,
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session, err := sessionStore.Get(r, "conduit")
				if err != nil {
					http.Error(w, "failed to get session", http.StatusInternalServerError)
					return
				}

				// User from session
				userID, ok := session.Values["userID"].(int64)
				if !ok {
					next.ServeHTTP(w, r)
					return
				}

				var user *zz.UserModel
				if err := db.ReadTX(r.Context(), func(tx *sqlite.Conn) error {
					user, err = zz.OnceReadByIDUser(tx, userID)
					if err != nil {
						return fmt.Errorf("failed to get user by ID: %w", err)
					}
					return nil
				}); err != nil {
					http.Error(w, "failed to get user by ID", http.StatusInternalServerError)
					return
				}

				ctx := ContextWithUser(r.Context(), user)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	)

	setupHomeRoutes(router, db)
	setupAuthRoutes(router, db, sessionStore)
	setupSettingsRoutes(router, db)
	setupUsersRoutes(router, db)
	setupArticlesRoutes(router, db)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	log.Printf("Stashing server on http://localhost%s", srv.Addr)

	go func() {
		<-setupCtx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.ListenAndServe()
}

// func userRequiredMiddleware(next http.Handler) http.Handler {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		if _, ok := UserFromContext(r.Context()); !ok {
// 			http.Error(w, "user required", http.StatusUnauthorized)
// 			return
// 		}
// 		next.ServeHTTP(w, r)
// 	})
// }

func SafeURL(format string, args ...interface{}) templ.SafeURL {
	return templ.SafeURL(fmt.Sprintf(format, args...))
}
