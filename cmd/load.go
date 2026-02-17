package cmd

import (
	"context"
	"fmt"
	"ikl/pkg/config"
	"ikl/pkg/harbor"
	"ikl/pkg/registry"
	"os"
	"strings"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	loadConfigPath string
	loadInputPath  string
)

var loadCmd = &cobra.Command{
	Use:     "load",
	Short:   "从离线 OCI Image Bundle 导入镜像到目标仓库",
	Example: `  ikl load --input my-bundle.tar --config config.yaml`,
	Run: func(cmd *cobra.Command, args []string) {
		if loadConfigPath == "" {
			handleError(fmt.Errorf("必须通过 --config 指定配置文件"))
		}
		if loadInputPath == "" {
			handleError(fmt.Errorf("必须通过 --input 指定输入文件路径"))
		}

		// 1. 加载配置
		cfg, err := config.LoadConfig(loadConfigPath)
		handleError(err)

		// 获取目标仓库配置
		dstRegistry, dstCfg, err := destinationConfig(cfg)
		handleError(err)

		// 2. 解包
		tmpDir, err := os.MkdirTemp("", "ikl-load-*")
		handleError(err)
		defer os.RemoveAll(tmpDir)

		fmt.Printf("📂 正在解压离线包 %s...\n", loadInputPath)
		err = registry.Untar(loadInputPath, tmpDir)
		handleError(err)

		// 3. 读取 OCI Layout
		lp := layout.Path(tmpDir)
		layoutImages, err := registry.GetLayoutImages(lp)
		handleError(err)

		if len(layoutImages) == 0 {
			fmt.Println("⚠️  离线包中未找到任何镜像")
			return
		}

		fmt.Printf("🚀 准备从离线包导入 %d 个镜像到 %s...\n", len(layoutImages), dstRegistry)

		// 4. 初始化 Harbor 客户端 (如果需要)
		var harborClient *harbor.Client
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
		}

		// 5. 初始化目标 Registry 客户端
		dstClient, err := registry.NewClient(
			normalizeURL(dstRegistry),
			dstCfg.Username,
			dstCfg.Password,
			dstCfg.Insecure,
			proxy,
			noProxy,
		)
		handleError(err)

		// 获取镜像列表以进行重命名匹配
		entries, _ := cfg.ResolveImages()
		entryMap := make(map[string]config.ImageEntry)
		for _, e := range entries {
			key := fmt.Sprintf("%s:%s", e.Name, e.Tags[0])
			entryMap[key] = e
		}

		successCount := 0
		failCount := 0
		ctx := context.Background()

		// 6. 遍历导入
		for i, lImg := range layoutImages {
			targetRepo := lImg.OriginalRepo
			targetTag := lImg.OriginalTag

			key := fmt.Sprintf("%s:%s", lImg.OriginalRepo, lImg.OriginalTag)
			if entry, ok := entryMap[key]; ok {
				if entry.TargetName != "" {
					targetRepo = entry.TargetName
				}
			}

			fmt.Printf("[%d/%d] 正在导入 %s:%s -> %s:%s ...\n", i+1, len(layoutImages), lImg.OriginalRepo, lImg.OriginalTag, targetRepo, targetTag)

			// --- Harbor 项目自动创建逻辑 ---
			if harborClient != nil {
				parts := strings.Split(targetRepo, "/")
				if len(parts) > 1 {
					project := parts[0]
					mu.Lock()
					if !checkedProjects[project] {
						_ = harborClient.EnsureProject(project)
						checkedProjects[project] = true
					}
					mu.Unlock()
				}
			}

			// 推送
			updates := make(chan v1.Update)
			bar := progressbar.DefaultBytes(-1, "   传输中")

			go func() {
				for update := range updates {
					if update.Total > 0 {
						bar.ChangeMax64(update.Total)
					}
					bar.Set64(update.Complete)
				}
			}()

			err = dstClient.LoadFromLayout(ctx, lp, lImg.Descriptor, targetRepo, targetTag, updates)

			func() {
				defer func() {
					if r := recover(); r != nil {
					}
				}()
				close(updates)
			}()

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

		fmt.Println("------------------------------------------------")
		fmt.Printf("🎉 任务结束。成功: %d, 失败: %d\n", successCount, failCount)
	},
}

func init() {
	rootCmd.AddCommand(loadCmd)
	loadCmd.Flags().StringVar(&loadConfigPath, "config", "", "配置文件路径")
	loadCmd.Flags().StringVar(&loadInputPath, "input", "", "输入 tar 文件路径")
}
