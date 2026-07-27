// Package postgres 是 Controller 持久化 Store 的 pgx/v5 组装边界。
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Database 拥有 Controller 唯一 pgx 连接池。
// R1 只实现启动 ping 和关闭，不提前定义 R7 业务 Store。
type Database struct {
	pool *pgxpool.Pool
}

// Open 解析 DSN、创建连接池并完成一次真实 Ping。
// 数据库不可用时 Controller 不得继续启动或进入 ready。
func Open(ctx context.Context, databaseURL string) (*Database, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, errors.New("ANYTTY_CLOUD_DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &Database{pool: pool}, nil
}

// Close 关闭 Controller 拥有的全部 PostgreSQL 连接。
func (database *Database) Close() {
	if database != nil && database.pool != nil {
		database.pool.Close()
	}
}
