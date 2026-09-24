package api

import (
	"context"
	"fmt"
	"github.com/example/go-starter-kit/internal/app/appfx"
	"os"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/platform/configenv"
	"github.com/spf13/cobra"
)

// NewCommand 每次构造独立命令树，配置在所选命令执行时才加载。
func NewCommand() *cobra.Command {
	serve := func(cmd *cobra.Command, _ []string) error {
		cfg, err := Load()
		if err != nil {
			return err
		}
		return Run(cmd.Context(), cfg)
	}
	root := &cobra.Command{Use: "api", Short: "API 服务与运维命令", Args: noArgs, RunE: serve, SilenceUsage: true, SilenceErrors: true}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return fmt.Errorf("命令行选项无效，请使用 --help") })
	root.AddCommand(&cobra.Command{Use: "serve", Short: "启动 HTTP 服务", Args: noArgs, RunE: serve})
	root.AddCommand(&cobra.Command{Use: "openapi", Short: "离线输出 OpenAPI 契约", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := OpenAPI()
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(append(data, '\n'))
		return err
	}})
	migrate := &cobra.Command{Use: "migrate", Short: "执行数据库迁移", Args: noArgs, RunE: func(*cobra.Command, []string) error { return fmt.Errorf("请指定 migrate up 或 migrate down") }}
	for _, direction := range []string{"up", "down"} {
		migrate.AddCommand(&cobra.Command{Use: direction, Short: map[string]string{"up": "应用待执行迁移", "down": "回滚一个迁移版本"}[direction], Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configenv.Parse[migrationConfig](os.LookupEnv)
			if err != nil {
				return err
			}
			if err := appfx.ValidateDatabaseURL(cfg.DatabaseURL); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			return db.Migrate(ctx, cfg.DatabaseURL, direction)
		}})
	}
	root.AddCommand(migrate)
	return root
}

type migrationConfig struct {
	DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("命令不接受额外参数，请使用 --help")
	}
	return nil
}
