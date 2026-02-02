package registry

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// TagDetail 包含镜像标签的详细信息
type TagDetail struct {
	Name          string
	Digest        string
	Architectures []string
	Size          int64
	Created       time.Time
	IsIndex       bool
}

type Client struct {
	URL           string
	Authenticator authn.Authenticator
	Transport     *http.Transport
	Insecure      bool
}

func NewClient(registryURL, username, password string, insecure bool, proxyURL string, noProxy string) (*Client, error) {
	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})

	t := remote.DefaultTransport.(*http.Transport).Clone()

	if insecure {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if proxyURL != "" {
		proxyEndpoint, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("无效的代理地址: %w", err)
		}

		noProxyList := strings.Split(noProxy, ",")
		for i := range noProxyList {
			noProxyList[i] = strings.TrimSpace(noProxyList[i])
		}

		t.Proxy = func(req *http.Request) (*url.URL, error) {
			host := req.URL.Hostname()
			for _, np := range noProxyList {
				if np == "" {
					continue
				}
				if host == np || strings.HasSuffix(host, "."+np) {
					return nil, nil
				}
			}
			return proxyEndpoint, nil
		}
	}

	return &Client{
		URL:           registryURL,
		Authenticator: auth,
		Transport:     t,
		Insecure:      insecure,
	}, nil
}

func (c *Client) GetOptions() []remote.Option {
	return []remote.Option{
		remote.WithAuth(c.Authenticator),
		remote.WithTransport(c.Transport),
	}
}

func (c *Client) ListRepositories(ctx context.Context) ([]string, error) {
	regOpts := []name.Option{}
	if c.Insecure {
		regOpts = append(regOpts, name.Insecure)
	}

	reg, err := name.NewRegistry(c.URL, regOpts...)
	if err != nil {
		return nil, fmt.Errorf("解析仓库地址失败: %w", err)
	}

	repos, err := remote.Catalog(ctx, reg, c.GetOptions()...)
	if err != nil {
		return nil, fmt.Errorf("获取 Catalog 失败 (请确保仓库启用了 Catalog API): %w", err)
	}
	return repos, nil
}

func (c *Client) ListTags(ctx context.Context, repoName string) ([]string, error) {
	refStr := fmt.Sprintf("%s/%s", c.URL, repoName)
	repoOpts := []name.Option{}
	if c.Insecure {
		repoOpts = append(repoOpts, name.Insecure)
	}

	repo, err := name.NewRepository(refStr, repoOpts...)
	if err != nil {
		return nil, fmt.Errorf("解析镜像名失败: %w", err)
	}

	tags, err := remote.List(repo, c.GetOptions()...)
	if err != nil {
		if tErr, ok := err.(*transport.Error); ok && tErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("镜像仓库未找到: %s", repoName)
		}
		return nil, err
	}
	return tags, nil
}

func (c *Client) GetTagDetail(ctx context.Context, repoName, tag string) (*TagDetail, error) {
	refStr := fmt.Sprintf("%s/%s:%s", c.URL, repoName, tag)
	refOpts := getNameOptions(c.Insecure)

	ref, err := name.ParseReference(refStr, refOpts...)
	if err != nil {
		return nil, err
	}

	desc, err := remote.Get(ref, c.GetOptions()...)
	if err != nil {
		return nil, err
	}

	detail := &TagDetail{
		Name:   tag,
		Digest: desc.Digest.String(),
		Size:   0,
	}

	if desc.MediaType.IsIndex() {
		detail.IsIndex = true
		idx, err := desc.ImageIndex()
		if err != nil {
			return detail, nil // 降级返回基础信息
		}
		manifest, err := idx.IndexManifest()
		if err == nil {
			for _, m := range manifest.Manifests {
				if m.Platform != nil && m.Platform.Architecture != "" && m.Platform.Architecture != "unknown" {
					arch := fmt.Sprintf("%s/%s", m.Platform.OS, m.Platform.Architecture)
					if m.Platform.Variant != "" {
						arch += "/" + m.Platform.Variant
					}
					exists := false
					for _, a := range detail.Architectures {
						if a == arch {
							exists = true
							break
						}
					}
					if !exists {
						detail.Architectures = append(detail.Architectures, arch)
					}
				}

				if detail.Created.IsZero() && m.Platform != nil && m.Platform.OS == "linux" {
					if img, err := idx.Image(m.Digest); err == nil {
						if cf, err := img.ConfigFile(); err == nil {
							detail.Created = cf.Created.Time
						}
					}
				}
			}
		}
	} else {
		img, err := desc.Image()
		if err == nil {
			config, err := img.ConfigFile()
			if err == nil {
				detail.Created = config.Created.Time
				detail.Architectures = []string{fmt.Sprintf("%s/%s", config.OS, config.Architecture)}
			}
			if layers, err := img.Layers(); err == nil {
				var size int64
				for _, l := range layers {
					s, _ := l.Size()
					size += s
				}
				detail.Size = size
			}
		}
	}

	return detail, nil
}

// CopyImage 支持进度条回调和架构筛选
// 修改：imageName 改为 srcRepo 和 dstRepo，允许重命名
func CopyImage(ctx context.Context, srcClient, dstClient *Client, srcRepo, dstRepo, tag string, progressCh chan<- v1.Update, platforms []string) error {
	srcRefStr := fmt.Sprintf("%s/%s:%s", srcClient.URL, srcRepo, tag)
	dstRefStr := fmt.Sprintf("%s/%s:%s", dstClient.URL, dstRepo, tag)

	srcRef, err := name.ParseReference(srcRefStr, getNameOptions(srcClient.Insecure)...)
	if err != nil {
		return fmt.Errorf("解析源镜像地址失败: %w", err)
	}

	dstRef, err := name.ParseReference(dstRefStr, getNameOptions(dstClient.Insecure)...)
	if err != nil {
		return fmt.Errorf("解析目标镜像地址失败: %w", err)
	}

	desc, err := remote.Get(srcRef, srcClient.GetOptions()...)
	if err != nil {
		return fmt.Errorf("拉取源镜像清单失败: %w", err)
	}

	writeOpts := dstClient.GetOptions()
	if progressCh != nil {
		writeOpts = append(writeOpts, remote.WithProgress(progressCh))
	}

	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return fmt.Errorf("解析 Image Index 失败: %w", err)
		}

		if len(platforms) > 0 {
			manifest, err := idx.IndexManifest()
			if err != nil {
				return err
			}

			var kept []v1.Descriptor
			for _, m := range manifest.Manifests {
				if m.Platform == nil {
					continue
				}
				for _, p := range platforms {
					if strings.Contains(m.Platform.Architecture, p) || strings.Contains(fmt.Sprintf("%s/%s", m.Platform.OS, m.Platform.Architecture), p) {
						kept = append(kept, m)
						break
					}
				}
			}

			if len(kept) == 0 {
				return fmt.Errorf("未找到符合架构 %v 的镜像", platforms)
			}

			if len(kept) == 1 {
				childImg, err := idx.Image(kept[0].Digest)
				if err != nil {
					return err
				}
				return remote.Write(dstRef, childImg, writeOpts...)
			}

			// 使用更新后的 filteredIndex
			idx = &filteredIndex{
				inner: idx,
				kept:  kept,
			}
		}

		err = remote.WriteIndex(dstRef, idx, writeOpts...)
		if err != nil {
			return fmt.Errorf("推送到目标仓库失败 (Index): %w", err)
		}
	} else {
		img, err := desc.Image()
		if err != nil {
			return fmt.Errorf("解析 Image 失败: %w", err)
		}

		if len(platforms) > 0 {
			cfg, err := img.ConfigFile()
			if err == nil {
				matched := false
				for _, p := range platforms {
					if strings.Contains(cfg.Architecture, p) {
						matched = true
						break
					}
				}
				if !matched {
					return fmt.Errorf("镜像架构 %s 不匹配目标 %v", cfg.Architecture, platforms)
				}
			}
		}

		err = remote.Write(dstRef, img, writeOpts...)
		if err != nil {
			return fmt.Errorf("推送到目标仓库失败 (Image): %w", err)
		}
	}

	return nil
}

func getNameOptions(insecure bool) []name.Option {
	if insecure {
		return []name.Option{name.Insecure}
	}
	return nil
}

// filteredIndex 包装原始 Index，仅返回筛选后的 Manifests
type filteredIndex struct {
	inner v1.ImageIndex
	kept  []v1.Descriptor
}

func (f *filteredIndex) MediaType() (types.MediaType, error) {
	return f.inner.MediaType()
}

func (f *filteredIndex) Digest() (v1.Hash, error) {
	b, err := f.RawManifest()
	if err != nil {
		return v1.Hash{}, err
	}
	h, _, err := v1.SHA256(bytes.NewReader(b))
	return h, err
}

func (f *filteredIndex) Size() (int64, error) {
	b, err := f.RawManifest()
	if err != nil {
		return 0, err
	}
	return int64(len(b)), nil
}

func (f *filteredIndex) IndexManifest() (*v1.IndexManifest, error) {
	orig, err := f.inner.IndexManifest()
	if err != nil {
		return nil, err
	}
	return &v1.IndexManifest{
		SchemaVersion: orig.SchemaVersion,
		MediaType:     orig.MediaType,
		Manifests:     f.kept,
		Annotations:   orig.Annotations,
	}, nil
}

func (f *filteredIndex) RawManifest() ([]byte, error) {
	m, err := f.IndexManifest()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func (f *filteredIndex) Image(h v1.Hash) (v1.Image, error) {
	return f.inner.Image(h)
}

func (f *filteredIndex) ImageIndex(h v1.Hash) (v1.ImageIndex, error) {
	return f.inner.ImageIndex(h)
}
