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
	"github.com/lozzow/termx/web-control/internal/httpapi"
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
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()
	go runRendezvousCleanupLoop(cleanupCtx, rendezvousService, cleanupTicker.C)
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
	rendezvousService := rendezvous.NewService(rendezvous.Config{DB: db, STUNServers: commaListEnv("TERMX_WEB_CONTROL_STUN_SERVERS")})
	api := httpapi.NewRouter(httpapi.Config{
		Accounts:        accounts,
		Machines:        machineService,
		Connect:         connectService,
		Rendezvous:      rendezvousService,
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

func runRendezvousCleanupLoop(ctx context.Context, cleaner rendezvousCleaner, ticks <-chan time.Time) {
	if cleaner == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if _, err := cleaner.CleanupExpired(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("cleanup expired rendezvous channels: %v", err)
			}
		}
	}
}
