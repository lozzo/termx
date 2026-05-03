package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/connect"
	"github.com/lozzow/termx/web-control/internal/deviceauth"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/hubregistry"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/rendezvous"
	"github.com/lozzow/termx/web-control/internal/store"
)

func main() {
	ctx := context.Background()
	db, err := openStoreFromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close sqlite: %v", err)
		}
	}()

	addr := os.Getenv("TERMX_WEB_CONTROL_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	log.Printf("termx web-control listening on http://%s", addr)
	router, err := newRouterFromServices(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	rendezvousService := rendezvous.NewService(rendezvous.Config{DB: db, STUNServers: commaListEnv("TERMX_WEB_CONTROL_STUN_SERVERS")})
	deviceAuthCleaner := deviceauth.NewService(deviceauth.Config{DB: db})
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()
	go runCleanupLoop(cleanupCtx, cleanupTicker.C, rendezvousService, cleanupFunc(func(ctx context.Context) (int64, error) {
		result, err := deviceAuthCleaner.CleanupExpired(ctx, deviceauth.CleanupInput{})
		return result.Expired + result.Deleted, err
	}))
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}

func openStoreFromEnv(ctx context.Context) (*sql.DB, error) {
	dsn := os.Getenv("TERMX_WEB_CONTROL_SQLITE_DSN")
	if dsn == "" {
		dsn = "file:termx-web-control.dev.db?_pragma=busy_timeout(5000)"
	}
	db, err := store.OpenSQLite(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func newAccountServiceFromEnv(ctx context.Context, db *sql.DB) (*account.Service, error) {
	_ = ctx
	secret := os.Getenv("TERMX_WEB_CONTROL_TOKEN_SECRET")
	if secret == "" {
		return nil, errors.New("TERMX_WEB_CONTROL_TOKEN_SECRET is required")
	}
	return account.NewService(account.Config{
		DB:     db,
		Tokens: account.NewHMACTokenIssuer([]byte(secret)),
	}), nil
}

func newRouterFromServices(ctx context.Context, db *sql.DB) (http.Handler, error) {
	accounts, err := newAccountServiceFromEnv(ctx, db)
	if err != nil {
		return nil, err
	}
	machineService := machines.NewService(machines.Config{DB: db})
	connectService := connect.NewService(connect.Config{DB: db})
	deviceAuthService := deviceauth.NewService(deviceauth.Config{DB: db, Accounts: accounts})
	rendezvousService := rendezvous.NewService(rendezvous.Config{DB: db, STUNServers: commaListEnv("TERMX_WEB_CONTROL_STUN_SERVERS")})
	hubRegistryService := hubregistry.NewService(hubregistry.Config{DB: db})
	api := httpapi.NewRouter(httpapi.Config{
		Accounts:        accounts,
		DeviceAuth:      deviceAuthService,
		Machines:        machineService,
		Connect:         connectService,
		Rendezvous:      rendezvousService,
		HubRegistry:     hubRegistryService,
		HubSharedSecret: os.Getenv("TERMX_WEB_CONTROL_HUB_SECRET"),
	})
	return withFrontendStaticFromEnv(os.Getenv("TERMX_WEB_CONTROL_STATIC_DIR"))(api), nil
}

func withFrontendStaticFromEnv(staticDir string) func(http.Handler) http.Handler {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return func(api http.Handler) http.Handler {
			return api
		}
	}
	return func(api http.Handler) http.Handler {
		return newFrontendStaticHandler(api, staticDir)
	}
}

func newFrontendStaticHandler(api http.Handler, staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))
	indexPath := filepath.Join(staticDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" {
			http.ServeFile(w, r, indexPath)
			return
		}
		if _, err := os.Stat(filepath.Join(staticDir, filepath.Clean(r.URL.Path))); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}

func commaListEnv(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

type rendezvousCleaner interface {
	CleanupExpired(context.Context) (int64, error)
}

type cleanupFunc func(context.Context) (int64, error)

func (f cleanupFunc) CleanupExpired(ctx context.Context) (int64, error) {
	return f(ctx)
}

func runRendezvousCleanupLoop(ctx context.Context, cleaner rendezvousCleaner, ticks <-chan time.Time) {
	runCleanupLoop(ctx, ticks, cleaner)
}

func runCleanupLoop(ctx context.Context, ticks <-chan time.Time, cleaners ...rendezvousCleaner) {
	filtered := cleaners[:0]
	for _, cleaner := range cleaners {
		if cleaner != nil {
			filtered = append(filtered, cleaner)
		}
	}
	if len(filtered) == 0 {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			for _, cleaner := range filtered {
				if _, err := cleaner.CleanupExpired(ctx); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("cleanup expired records: %v", err)
				}
			}
		}
	}
}
