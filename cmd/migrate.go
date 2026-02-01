package cmd

import (
	"context"
	"fmt"
	"ikl/pkg/config"
	"ikl/pkg/registry"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var configPath string

var migrateCmd = &cobra.Command{
	Use:     "migrate",
	Short:   "根据配置文件批量迁移镜像",
	Long:    `读取 YAML 配置文件，将镜像从源仓库复制到目标仓库。会自动识别 Manifest List 从而支持多架构迁移。`,
	Example: `  ikl migrate --config config.yaml --proxy http://127.0.0.1:7890`,
	Run: func(cmd *cobra.Command, args []string) {
		if configPath == "" {
			handleError(fmt.Errorf("请提供配置文件路径"))
		}

		// 1. 加载配置
		cfg, err := config.LoadConfig(configPath)
		handleError(err)

		fmt.Println("🚀 开始执行镜像迁移任务...")
		fmt.Printf("源仓库: %s (Insecure: %v)\n", cfg.Source.Registry, cfg.Source.Insecure)
		fmt.Printf("目标仓库: %s (Insecure: %v)\n", cfg.Destination.Registry, cfg.Destination.Insecure)

		if proxy != "" {
			fmt.Printf("🌐 全局代理: %s\n", proxy)
			if noProxy != "" {
				fmt.Printf("🛑 排除代理 (NoProxy): %s\n", noProxy)
			}
		}
		fmt.Println("------------------------------------------------")

		// 2. 初始化客户端
		srcClient, err := registry.NewClient(
			normalizeURL(cfg.Source.Registry),
			cfg.Source.Username,
			cfg.Source.Password,
			cfg.Source.Insecure,
			proxy,
			noProxy,
		)
		handleError(err)

		dstClient, err := registry.NewClient(
			normalizeURL(cfg.Destination.Registry),
			cfg.Destination.Username,
			cfg.Destination.Password,
			cfg.Destination.Insecure,
			proxy,
			noProxy,
		)
		handleError(err)

		ctx := context.Background()
		successCount := 0
		failCount := 0

		// 3. 遍历镜像列表
		for _, img := range cfg.Images {
			// 如果配置中未指定 Tags，则自动获取源仓库所有 Tags
			tagsToMigrate := img.Tags
			if len(tagsToMigrate) == 0 {
				fmt.Printf("🔍 未指定 Tag，正在获取 %s 的所有 Tag...\n", img.Name)
				fetchedTags, err := srcClient.ListTags(ctx, img.Name)
				if err != nil {
					fmt.Printf("❌ 获取 Tag 失败 [%s]: %v\n", img.Name, err)
					failCount++
					continue
				}
				tagsToMigrate = fetchedTags
			}

			if len(img.Architectures) > 0 {
				fmt.Printf("🎯 镜像 %s 指定架构: %v\n", img.Name, img.Architectures)
			}

			// 4. 执行迁移
			for _, tag := range tagsToMigrate {
				fmt.Printf("⏳ 正在迁移 %s:%s ...\n", img.Name, tag)

				// 创建进度条通道
				updates := make(chan v1.Update)
				errCh := make(chan error, 1)

				// 创建进度条
				bar := progressbar.DefaultBytes(
					-1,
					"   传输中",
				)

				// 启动 goroutine 监听进度
				go func() {
					for update := range updates {
						if update.Total > 0 {
							bar.ChangeMax64(update.Total)
						}
						bar.Set64(update.Complete)
					}
				}()

				// 启动迁移
				go func() {
					// 传入 img.Architectures，实现按镜像配置过滤
					err := registry.CopyImage(ctx, srcClient, dstClient, img.Name, tag, updates, img.Architectures)

					// 安全关闭 channel
					func() {
						defer func() {
							if r := recover(); r != nil {
							}
						}()
						close(updates)
					}()

					errCh <- err
				}()

				// 等待迁移完成
				err = <-errCh

				// 确保进度条完成显示
				_ = bar.Finish()
				fmt.Println() // 进度条换行

				if err != nil {
					fmt.Printf("   ❌ 失败: %v\n", err)
					failCount++
				} else {
					fmt.Printf("   ✅ 完成\n")
					successCount++
				}
			}
		}

		fmt.Println("------------------------------------------------")
		fmt.Printf("🎉 任务结束。成功: %d, 失败: %d\n", successCount, failCount)
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "迁移配置文件路径")
}

func normalizeURL(u string) string {
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	return strings.TrimSuffix(u, "/")
}
