package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/httpapi"
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
	accounts, err := newAccountServiceFromEnv(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	if err := http.ListenAndServe(addr, httpapi.NewRouter(httpapi.Config{Accounts: accounts})); err != nil {
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
