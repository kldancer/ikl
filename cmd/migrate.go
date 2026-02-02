package cmd

import (
	"context"
	"fmt"
	"ikl/pkg/config"
	"ikl/pkg/harbor"
	"ikl/pkg/registry"
	"strings"
	"sync"

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
		fmt.Printf("目标仓库: %s (Type: %s, Insecure: %v)\n", cfg.Destination.Registry, cfg.Destination.Type, cfg.Destination.Insecure)

		if proxy != "" {
			fmt.Printf("🌐 全局代理: %s\n", proxy)
			if noProxy != "" {
				fmt.Printf("🛑 排除代理 (NoProxy): %s\n", noProxy)
			}
		}
		fmt.Println("------------------------------------------------")

		// 初始化 Harbor 客户端 (如果需要)
		var harborClient *harbor.Client
		// 用于缓存已检查过的项目，避免重复调用 API
		checkedProjects := make(map[string]bool)
		var mu sync.Mutex

		if strings.ToLower(cfg.Destination.Type) == "harbor" {
			hClient, err := harbor.NewClient(
				cfg.Destination.Registry,
				cfg.Destination.Username,
				cfg.Destination.Password,
				cfg.Destination.Insecure,
				proxy,
				noProxy,
			)
			if err != nil {
				handleError(fmt.Errorf("初始化 Harbor 客户端失败: %v", err))
			}
			harborClient = hClient
			fmt.Println("⚓️ 已启用 Harbor 自动项目管理")
		}

		// 2. 初始化 Registry 客户端
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
			dstName := img.TargetName
			if dstName == "" {
				dstName = img.Name
			}

			// --- Harbor 项目自动创建逻辑 ---
			if harborClient != nil {
				// 提取项目名称 (例如 "rook/ceph" -> "rook")
				parts := strings.Split(dstName, "/")
				if len(parts) > 1 {
					project := parts[0]

					mu.Lock()
					if !checkedProjects[project] {
						err := harborClient.EnsureProject(project)
						if err != nil {
							fmt.Printf("⚠️  无法自动创建/检查 Harbor 项目 '%s': %v\n", project, err)
							// 不终止程序，尝试继续推送，也许项目已经存在只是 API 权限问题
						}
						checkedProjects[project] = true
					}
					mu.Unlock()
				}
			}
			// --------------------------------

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
				fmt.Printf("🎯 镜像 %s (-> %s) 指定架构: %v\n", img.Name, dstName, img.Architectures)
			}

			// 4. 执行迁移
			for _, tag := range tagsToMigrate {
				fmt.Printf("⏳ 正在迁移 %s:%s -> %s:%s ...\n", img.Name, tag, dstName, tag)

				updates := make(chan v1.Update)
				errCh := make(chan error, 1)

				bar := progressbar.DefaultBytes(
					-1,
					"   传输中",
				)

				go func() {
					for update := range updates {
						if update.Total > 0 {
							bar.ChangeMax64(update.Total)
						}
						bar.Set64(update.Complete)
					}
				}()

				go func() {
					err := registry.CopyImage(ctx, srcClient, dstClient, img.Name, dstName, tag, updates, img.Architectures)

					func() {
						defer func() {
							if r := recover(); r != nil {
							}
						}()
						close(updates)
					}()

					errCh <- err
				}()

				err = <-errCh
				_ = bar.Finish()
				fmt.Println()

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
