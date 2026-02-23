package cmd

import (
	"Imt/pkg/config"
	"Imt/pkg/registry"
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	saveConfigPath string
	saveOutputPath string
	saveWorkspace  string
	maxRetries     = 3
)

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "从源仓库导出镜像并保存为离线 OCI Image Bundle",
	Long: `从源仓库导出镜像并保存为离线 OCI Image Bundle。
支持通过 --workspace 参数指定持久化工作目录，实现断点续传功能：当任务中途出错或停止时，
只需指定相同的工作目录重新运行，程序会自动跳过已导出的镜像。`,
	Example: `  Imt save --config config.yaml --output my-bundle.tar
  Imt save --config config.yaml --output my-bundle.tar --workspace ./my-save-work --proxy http://127.0.0.1:7890`,
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

		// 创建临时目录或使用指定的 Workspace
		var tmpDir string
		if saveWorkspace != "" {
			tmpDir = saveWorkspace
			if err := os.MkdirAll(tmpDir, 0755); err != nil {
				handleError(fmt.Errorf("创建工作目录失败: %v", err))
			}
			fmt.Printf("📁 使用工作目录: %s (支持断点续传)\n", tmpDir)
		} else {
			dir, err := os.MkdirTemp("", "Imt-save-*")
			handleError(err)
			tmpDir = dir
			defer os.RemoveAll(tmpDir)
		}

		if proxy != "" {
			fmt.Printf("🌐 全局代理: %s\n", proxy)
			if noProxy != "" {
				fmt.Printf("🛑 排除代理 (NoProxy): %s\n", noProxy)
			}
		}

		fmt.Printf("📦 开始导出 %d 个镜像到离线包...\n", len(entries))

		// 我们可能需要根据 ImageEntry 中的 Registry 寻找对应的 SourceRegistry 配置（认证信息）
		for i, entry := range entries {
			fmt.Printf("[%d/%d] 正在处理 %s/%s:%s (%v)...\n", i+1, len(entries), entry.Registry, entry.Name, entry.Tags[0], entry.Architectures)

			// 检查是否已经存在 (断点续传)
			exists, err := registry.ImageExists(tmpDir, entry.Name, entry.Tags[0])
			if err == nil && exists {
				fmt.Println("   ⏭️  已存在，跳过")
				continue
			}

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

			// 重试逻辑
			success := false
			for attempt := 1; attempt <= maxRetries; attempt++ {
				err = client.SaveToLayout(context.Background(), entry.Name, entry.Tags[0], tmpDir, entry.Architectures)
				if err == nil {
					success = true
					break
				}
				fmt.Printf("   ⚠️  第 %d 次尝试失败: %v\n", attempt, err)
				if attempt < maxRetries {
					fmt.Printf("   🔄 正在重试...\n")
				}
			}

			if !success {
				handleError(fmt.Errorf("❌ 镜像 %s 导出最终失败: %v\n", entry.Name, err))
			}
			fmt.Println("   ✅ 完成")
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
	saveCmd.Flags().StringVar(&saveWorkspace, "workspace", "", "指定本地工作目录（保存 OCI Layout），用于断点续传")
}
