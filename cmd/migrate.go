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

		images, err := cfg.ResolveImages()
		handleError(err)

		fmt.Println("🚀 开始执行镜像迁移任务...")
		printSourceRegistries(cfg, images)
		dstRegistry, dstCfg, err := destinationConfig(cfg)
		handleError(err)
		fmt.Printf("目标仓库: %s (Type: %s, Insecure: %v)\n", dstRegistry, dstCfg.Type, dstCfg.Insecure)

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

		if strings.ToLower(dstCfg.Type) == "harbor" {
			hClient, err := harbor.NewClient(
				dstRegistry,
				dstCfg.Username,
				dstCfg.Password,
				dstCfg.Insecure,
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
		srcClients := make(map[string]*registry.Client)

		dstClient, err := registry.NewClient(
			normalizeURL(dstRegistry),
			dstCfg.Username,
			dstCfg.Password,
			dstCfg.Insecure,
			proxy,
			noProxy,
		)
		handleError(err)

		ctx := context.Background()
		successCount := 0
		failCount := 0

		// 3. 遍历镜像列表
		for _, img := range images {
			registryURL := normalizeURL(img.Registry)

			srcClient, ok := srcClients[registryURL]
			if !ok {
				srcCfg := sourceConfigForRegistry(cfg, registryURL)
				client, err := registry.NewClient(
					registryURL,
					srcCfg.Username,
					srcCfg.Password,
					srcCfg.Insecure,
					proxy,
					noProxy,
				)
				handleError(err)
				srcClients[registryURL] = client
				srcClient = client
			}

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

func sourceConfigForRegistry(cfg *config.MigrateConfig, registryURL string) config.RegistryConfig {
	if cfg.SourceRegistries != nil {
		if regCfg, ok := cfg.SourceRegistries[registryURL]; ok {
			return withRegistryFallback(regCfg, registryURL)
		}
		normalizedRegistry := normalizeURL(registryURL)
		for key, regCfg := range cfg.SourceRegistries {
			if normalizeURL(key) == normalizedRegistry {
				return withRegistryFallback(regCfg, registryURL)
			}
		}
	}

	return withRegistryFallback(config.RegistryConfig{}, registryURL)
}

func withRegistryFallback(cfg config.RegistryConfig, registryURL string) config.RegistryConfig {
	if cfg.Registry == "" {
		cfg.Registry = registryURL
	}
	return cfg
}

func printSourceRegistries(cfg *config.MigrateConfig, images []config.ImageEntry) {
	registrySet := make(map[string]struct{})
	for _, img := range images {
		registry := normalizeURL(img.Registry)
		if registry != "" {
			registrySet[registry] = struct{}{}
		}
	}

	if len(registrySet) == 0 {
		fmt.Println("源仓库: (未指定)")
		return
	}

	fmt.Println("源仓库列表:")
	for registryURL := range registrySet {
		regCfg := sourceConfigForRegistry(cfg, registryURL)
		authLabel := "匿名"
		if regCfg.Username != "" || regCfg.Password != "" {
			authLabel = "需要认证"
		}
		fmt.Printf("  - %s (Insecure: %v, %s)\n", registryURL, regCfg.Insecure, authLabel)
	}
}

func destinationConfig(cfg *config.MigrateConfig) (string, config.RegistryConfig, error) {
	if len(cfg.DestinationRegs) == 0 {
		return "", config.RegistryConfig{}, fmt.Errorf("destination_registries 不能为空")
	}
	if len(cfg.DestinationRegs) > 1 {
		return "", config.RegistryConfig{}, fmt.Errorf("destination_registries 仅支持配置一个目标仓库")
	}
	for registry, regCfg := range cfg.DestinationRegs {
		registry = normalizeURL(registry)
		return registry, withRegistryFallback(regCfg, registry), nil
	}
	return "", config.RegistryConfig{}, fmt.Errorf("destination_registries 未配置有效目标仓库")
}
