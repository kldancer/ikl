package cmd

import (
	"Imt/pkg/registry"
	"Imt/pkg/ui"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	registryURL string
	username    string
	password    string
	repoName    string
	insecure    bool
)

var listImagesCmd = &cobra.Command{
	Use:     "list-images",
	Short:   "列出仓库中的所有镜像名称",
	Example: `  Imt list-images --registry registry.example.com --user admin --pass 123456 --proxy http://127.0.0.1:7890`,
	Run: func(cmd *cobra.Command, args []string) {
		validateRegistryArgs()

		client, err := registry.NewClient(registryURL, username, password, insecure, proxy, noProxy)
		handleError(err)

		fmt.Printf("🔍 正在连接仓库 %s 获取目录...\n", registryURL)

		repos, err := client.ListRepositories(context.Background())
		if err != nil {
			// 针对 Harbor 等仓库禁用 Catalog API 的情况进行友好提示
			if strings.Contains(err.Error(), "UNAUTHORIZED") || strings.Contains(err.Error(), "unauthorized") {
				fmt.Println("❌ 权限验证失败，或服务端拒绝了 Catalog 请求。")
				fmt.Println("💡 提示：")
				fmt.Println("   1. 请检查账号密码是否正确。")
				fmt.Println("   2. 如果这是 Harbor 仓库，Harbor 默认禁用了 Docker 原生 Catalog API (/v2/_catalog)。")
				fmt.Println("      这会导致无法使用 list-images 列出所有镜像，但 list-tags 和 migrate 功能不受影响。")
				fmt.Println("      (请在 config.yaml 中直接指定具体的镜像名称进行迁移)")
				os.Exit(1)
			}
			handleError(err)
		}

		if len(repos) == 0 {
			fmt.Println("⚠️  仓库为空或无权查看目录。")
			return
		}

		var data [][]string
		for i, repo := range repos {
			data = append(data, []string{fmt.Sprintf("%d", i+1), repo})
		}

		ui.RenderTable([]string{"序号", "镜像仓库名称 (Repository)"}, data)
		fmt.Printf("\n共找到 %d 个镜像仓库。\n", len(repos))
	},
}

var listTagsCmd = &cobra.Command{
	Use:     "list-tags",
	Short:   "列出指定镜像的所有标签详情",
	Example: `  Imt list-tags --registry registry.example.com --repo my-app/worker --insecure --proxy http://127.0.0.1:7890`,
	Run: func(cmd *cobra.Command, args []string) {
		validateRegistryArgs()
		if repoName == "" {
			handleError(fmt.Errorf("必须通过 --repo 指定镜像名称"))
		}

		client, err := registry.NewClient(registryURL, username, password, insecure, proxy, noProxy)
		handleError(err)

		fmt.Printf("🔍 正在获取 %s/%s 的标签列表...\n", registryURL, repoName)

		tags, err := client.ListTags(context.Background(), repoName)
		handleError(err)

		if len(tags) == 0 {
			fmt.Println("⚠️  该镜像没有标签。")
			return
		}

		sort.Strings(tags)

		fmt.Printf("📋 共找到 %d 个标签，正在获取详细信息 (并发数: 10)...\n", len(tags))

		type result struct {
			index int
			info  *registry.TagDetail
			err   error
		}

		resultsCh := make(chan result, len(tags))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup

		for i, tag := range tags {
			wg.Add(1)
			go func(idx int, t string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				info, err := client.GetTagDetail(context.Background(), repoName, t)
				resultsCh <- result{index: idx, info: info, err: err}
			}(i, tag)
		}

		go func() {
			wg.Wait()
			close(resultsCh)
		}()

		detailsMap := make(map[string]*registry.TagDetail)
		for res := range resultsCh {
			if res.err == nil {
				detailsMap[tags[res.index]] = res.info
			} else {
				detailsMap[tags[res.index]] = &registry.TagDetail{Name: tags[res.index]}
			}
		}

		var data [][]string
		for i, tag := range tags {
			info := detailsMap[tag]

			displayName := tag
			if tag == "latest" {
				displayName += " (*)"
			}

			archStr := "-"
			if len(info.Architectures) > 0 {
				joined := strings.Join(info.Architectures, ", ")
				if len(joined) > 50 {
					archStr = joined[:47] + "..."
				} else {
					archStr = joined
				}
			} else if info.IsIndex {
				archStr = "Multi-arch"
			}

			sizeStr := formatBytes(info.Size)
			if info.IsIndex {
				sizeStr = "Index"
			}

			timeStr := "-"
			if !info.Created.IsZero() {
				timeStr = info.Created.Local().Format("2006-01-02 15:04")
			}

			data = append(data, []string{
				fmt.Sprintf("%d", i+1),
				displayName,
				archStr,
				sizeStr,
				timeStr,
			})
		}

		ui.RenderTable([]string{"序号", "标签 (TAG)", "架构 (ARCH)", "大小 (SIZE)", "创建时间 (CREATED)"}, data)
		fmt.Printf("\n镜像 %s 共找到 %d 个标签。\n", repoName, len(tags))
	},
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func init() {
	rootCmd.AddCommand(listImagesCmd)
	rootCmd.AddCommand(listTagsCmd)

	listImagesCmd.Flags().StringVar(&registryURL, "registry", "", "仓库地址 (如 localhost:5000)")
	listImagesCmd.Flags().StringVarP(&username, "username", "u", "", "用户名")
	listImagesCmd.Flags().StringVarP(&password, "password", "p", "", "密码")
	listImagesCmd.Flags().BoolVar(&insecure, "insecure", false, "允许 HTTP 或跳过 TLS 验证")
	listImagesCmd.MarkFlagRequired("registry")

	listTagsCmd.Flags().StringVar(&registryURL, "registry", "", "仓库地址")
	listTagsCmd.Flags().StringVar(&repoName, "repo", "", "镜像名称 (如 library/nginx)")
	listTagsCmd.Flags().StringVarP(&username, "username", "u", "", "用户名")
	listTagsCmd.Flags().StringVarP(&password, "password", "p", "", "密码")
	listTagsCmd.Flags().BoolVar(&insecure, "insecure", false, "允许 HTTP 或跳过 TLS 验证")
	listTagsCmd.MarkFlagRequired("registry")
	listTagsCmd.MarkFlagRequired("repo")
}

func validateRegistryArgs() {
	registryURL = strings.TrimPrefix(registryURL, "http://")
	registryURL = strings.TrimPrefix(registryURL, "https://")
	registryURL = strings.TrimSuffix(registryURL, "/")
}
