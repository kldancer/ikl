package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// 定义全局变量，供子命令使用
var (
	proxy   string
	noProxy string // 新增：不使用代理的主机列表
)

var rootCmd = &cobra.Command{
	Use:   "ikl",
	Short: "IKL - 简易高效的容器镜像管理工具",
	Long: `IKL 是一个用于管理 Docker 私有仓库的命令行工具。
支持查看镜像列表、Tag 列表，以及支持多架构（AMD64/ARM64）的镜像迁移。`,
}

// Execute 是命令行的入口
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// 添加全局 Persistent Flag
	rootCmd.PersistentFlags().StringVar(&proxy, "proxy", "", "HTTP/HTTPS 代理地址 (例如: http://127.0.0.1:7890)")
	// 新增 flag
	rootCmd.PersistentFlags().StringVar(&noProxy, "no-proxy", "", "不使用代理的主机列表，逗号分隔 (例如: ykl.io,localhost,127.0.0.1)")
}

// handleError 统一错误处理
func handleError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 错误: %v\n", err)
		os.Exit(1)
	}
}
