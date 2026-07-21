// Package postgrestest 为 private Cloud 测试提供隔离的 PostgreSQL schema fixture。
//
// 测试传入稳定 key 后可以关闭并重新打开同一数据库，以验证重启恢复；不同 key 和不同
// test process 使用独立 schema。该包不是生产 Store factory，运行时不得 import。
package postgrestest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
)

type fixture struct {
	dsn   string
	admin *sql.DB
}

var (
	fixturesMu             sync.Mutex
	fixtures               = make(map[string]fixture)
	errDatabaseUnavailable = errors.New("PostgreSQL integration database is unavailable")
)

// Open 打开 key 对应的隔离 PostgreSQL schema。
// MUXVIA_TEST_POSTGRES_DSN 未设置时使用本地 55432 端口；数据库不可达则跳过当前测试。
func Open(t testing.TB, key string) (*cloudpostgres.Store, error) {
	t.Helper()
	return cloudpostgres.Open(context.Background(), DSN(t, key))
}

// DSN 返回 key 对应的隔离 schema 连接串，供需要自行启动 Controller 的测试装配使用。
// DSN 只可写入测试临时配置，不能进入日志、manifest 或测试失败输出。
func DSN(t testing.TB, key string) string {
	t.Helper()
	fixturesMu.Lock()
	value, ok := fixtures[key]
	if !ok {
		created, err := createFixture(key)
		if err != nil {
			fixturesMu.Unlock()
			if errors.Is(err, errDatabaseUnavailable) {
				t.Skipf("PostgreSQL integration database is unavailable: %v", err)
			}
			t.Fatal(err)
		}
		value = created
		fixtures[key] = value
		t.Cleanup(func() { cleanup(key) })
	}
	fixturesMu.Unlock()
	return value.dsn
}

func createFixture(key string) (fixture, error) {
	base := os.Getenv("MUXVIA_TEST_POSTGRES_DSN")
	if base == "" {
		base = "postgres://127.0.0.1:55432/postgres?sslmode=disable"
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		return fixture{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		admin.Close()
		return fixture{}, fmt.Errorf("%w: %v", errDatabaseUnavailable, err)
	}
	digest := sha256.Sum256([]byte(key + "\x00" + os.Getenv("GO_TEST_SHARD_INDEX")))
	schema := "muxvia_test_" + hex.EncodeToString(digest[:8])
	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		admin.Close()
		return fixture{}, err
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		return fixture{}, err
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || !strings.HasPrefix(parsed.Scheme, "postgres") {
		admin.Close()
		return fixture{}, fmt.Errorf("MUXVIA_TEST_POSTGRES_DSN must be a PostgreSQL URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return fixture{dsn: parsed.String(), admin: admin}, nil
}

func cleanup(key string) {
	fixturesMu.Lock()
	value, ok := fixtures[key]
	delete(fixtures, key)
	fixturesMu.Unlock()
	if !ok {
		return
	}
	parsed, err := url.Parse(value.dsn)
	if err == nil {
		schema := parsed.Query().Get("search_path")
		if strings.HasPrefix(schema, "muxvia_test_") {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = value.admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
			cancel()
		}
	}
	_ = value.admin.Close()
}
