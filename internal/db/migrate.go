// Package db 管理连接池与嵌入式版本迁移。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate 作为独立发布步骤运行，API 启动时不自动迁移。
func Migrate(ctx context.Context, url, command string) error {
	database, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("connect migration database: %w", err)
	}
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database, files)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	switch command {
	case "up":
		_, err = provider.Up(ctx)
	case "down":
		_, err = provider.Down(ctx)
	default:
		return fmt.Errorf("unsupported migration command %q (use up or down)", command)
	}
	return err
}
