package worker

import (
	"fmt"
	"github.com/spf13/cobra"
)

// NewCommand 只在真正启动 Worker 时解析其配置。
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "worker", Short: "启动后台进程", SilenceUsage: true, SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("Worker 不接受位置参数，请使用 --help")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := Load()
			if err != nil {
				return err
			}
			return Run(cmd.Context(), cfg)
		}}
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetFlagErrorFunc(func(*cobra.Command, error) error { return fmt.Errorf("命令行选项无效，请使用 --help") })
	return cmd
}
