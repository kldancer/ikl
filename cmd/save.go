package cmd

import (
	"context"
	"fmt"
	"ikl/pkg/config"
	"ikl/pkg/registry"
	"os"

	"github.com/spf13/cobra"
)

var (
	saveConfigPath string
	saveOutputPath string
)

var saveCmd = &cobra.Command{
	Use:     "save",
	Short:   "从源仓库导出镜像并保存为离线 OCI Image Bundle",
	Example: `  ikl save --config config.yaml --output my-bundle.tar --proxy http://127.0.0.1:7890`,
	Run: func(cmd *cobra.Command, args []string) {
		if saveConfigPath == "" {
			handleError(fmt.Errorf("必须通过 --config 指定配置文件"))
		}
		if saveOutputPath == "" {
			handleError(fmt.Errorf("必须通过 --output 指定输出文件路径"))
		}

		cfg, err := config.LoadConfig(saveConfigPath)
		handleError(err)

		entries, err := cfg.ResolveImages()
		handleError(err)

		if len(entries) == 0 {
			fmt.Println("⚠️  镜像列表为空，请检查配置文件中的 image_list")
			return
		}

		// 创建临时目录存放 OCI Layout
		tmpDir, err := os.MkdirTemp("", "ikl-save-*")
		handleError(err)
		defer os.RemoveAll(tmpDir)

		if proxy != "" {
			fmt.Printf("🌐 全局代理: %s\n", proxy)
			if noProxy != "" {
				fmt.Printf("🛑 排除代理 (NoProxy): %s\n", noProxy)
			}
		}

		fmt.Printf("📦 开始导出 %d 个镜像到离线包...\n", len(entries))

		// 我们可能需要根据 ImageEntry 中的 Registry 寻找对应的 SourceRegistry 配置（认证信息）
		for i, entry := range entries {
			fmt.Printf("[%d/%d] 正在导出 %s/%s:%s (%v)...\n", i+1, len(entries), entry.Registry, entry.Name, entry.Tags[0], entry.Architectures)

			srcRegCfg, ok := cfg.SourceRegistries[entry.Registry]
			var srcUser, srcPass string
			var srcInsecure bool
			if ok {
				srcUser = srcRegCfg.Username
				srcPass = srcRegCfg.Password
				srcInsecure = srcRegCfg.Insecure
			}

			client, err := registry.NewClient(entry.Registry, srcUser, srcPass, srcInsecure, proxy, noProxy)
			if err != nil {
				handleError(fmt.Errorf("创建客户端失败 %s: %v\n", entry.Registry, err))
			}

			err = client.SaveToLayout(context.Background(), entry.Name, entry.Tags[0], tmpDir, entry.Architectures)
			if err != nil {
				handleError(fmt.Errorf("导出失败: %v\n", err))
			}
		}

		fmt.Printf("🗜️  正在打包为 %s...\n", saveOutputPath)
		err = registry.Tar(tmpDir, saveOutputPath)
		handleError(err)

		fmt.Printf("✅ 离线包已导出至: %s\n", saveOutputPath)
	},
}

func init() {
	rootCmd.AddCommand(saveCmd)

	saveCmd.Flags().StringVar(&saveConfigPath, "config", "", "配置文件路径")
	saveCmd.Flags().StringVar(&saveOutputPath, "output", "", "输出 tar 文件路径")
}
