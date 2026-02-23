package registry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// SaveToLayout 从源仓库拉取镜像并保存到本地 OCI Layout 目录
func (c *Client) SaveToLayout(ctx context.Context, repo, tag string, path string, platforms []string) error {
	refStr := fmt.Sprintf("%s/%s:%s", c.URL, repo, tag)
	ref, err := name.ParseReference(refStr, getNameOptions(c.Insecure)...)
	if err != nil {
		return fmt.Errorf("解析镜像地址失败: %w", err)
	}

	desc, err := remote.Get(ref, c.GetOptions()...)
	if err != nil {
		return fmt.Errorf("拉取镜像清单失败: %w", err)
	}

	var lp layout.Path
	if _, err := os.Stat(filepath.Join(path, "oci-layout")); os.IsNotExist(err) {
		lp, err = layout.Write(path, empty.Index)
		if err != nil {
			return fmt.Errorf("初始化 OCI Layout 失败: %w", err)
		}
	} else {
		lp = layout.Path(path)
	}

	annotations := map[string]string{
		"org.opencontainers.image.ref.name": fmt.Sprintf("%s/%s:%s", c.URL, repo, tag),
		"Imt.original.repo":                 repo,
		"Imt.original.tag":                  tag,
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

			idx = &filteredIndex{
				inner: idx,
				kept:  kept,
			}
		}

		return lp.AppendIndex(idx, layout.WithAnnotations(annotations))
	}

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

	return lp.AppendImage(img, layout.WithAnnotations(annotations))
}

// LayoutImage 包含从 OCI Layout 中读取的镜像信息
type LayoutImage struct {
	OriginalRepo string
	OriginalTag  string
	Annotations  map[string]string
	Descriptor   v1.Descriptor
}

// GetLayoutImages 获取 OCI Layout 中的所有镜像信息
func GetLayoutImages(path string) ([]LayoutImage, error) {
	if _, err := os.Stat(filepath.Join(path, "oci-layout")); os.IsNotExist(err) {
		return nil, nil
	}

	lp := layout.Path(path)
	index, err := lp.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("读取 OCI Layout 失败: %w", err)
	}

	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("获取 Index Manifest 失败: %w", err)
	}

	var images []LayoutImage
	for _, m := range manifest.Manifests {
		img := LayoutImage{
			OriginalRepo: m.Annotations["Imt.original.repo"],
			OriginalTag:  m.Annotations["Imt.original.tag"],
			Annotations:  m.Annotations,
			Descriptor:   m,
		}
		images = append(images, img)
	}

	return images, nil
}

// ImageExists 检查镜像是否已存在于 OCI Layout 中
func ImageExists(path string, repo, tag string) (bool, error) {
	images, err := GetLayoutImages(path)
	if err != nil {
		return false, err
	}

	for _, img := range images {
		if img.OriginalRepo == repo && img.OriginalTag == tag {
			return true, nil
		}
	}
	return false, nil
}

// LoadFromLayout 从本地 OCI Layout 目录读取指定镜像并推送到目标仓库
func (c *Client) LoadFromLayout(ctx context.Context, path string, desc v1.Descriptor, targetRepo, targetTag string, progressCh chan<- v1.Update) error {
	targetRefStr := fmt.Sprintf("%s/%s:%s", c.URL, targetRepo, targetTag)
	targetRef, err := name.ParseReference(targetRefStr, getNameOptions(c.Insecure)...)
	if err != nil {
		return fmt.Errorf("解析目标镜像地址失败: %w", err)
	}

	lp := layout.Path(path)
	rootIdx, err := lp.ImageIndex()
	if err != nil {
		return fmt.Errorf("读取 OCI Layout Root Index 失败: %w", err)
	}

	var item interface{}
	if desc.MediaType.IsIndex() {
		item, err = rootIdx.ImageIndex(desc.Digest)
	} else if desc.MediaType.IsImage() {
		item, err = rootIdx.Image(desc.Digest)
	} else {
		return fmt.Errorf("未知 MediaType: %s", desc.MediaType)
	}

	if err != nil {
		return fmt.Errorf("从 Layout 获取镜像失败 (Digest: %s): %w", desc.Digest, err)
	}

	writeOpts := c.GetOptions()
	if progressCh != nil {
		writeOpts = append(writeOpts, remote.WithProgress(progressCh))
	}

	switch v := item.(type) {
	case v1.ImageIndex:
		return remote.WriteIndex(targetRef, v, writeOpts...)
	case v1.Image:
		return remote.Write(targetRef, v, writeOpts...)
	default:
		return fmt.Errorf("未知镜像类型")
	}
}

// Tar 打包目录
func Tar(srcDir, dstFile string) error {
	f, err := os.Create(dstFile)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	})
}

// Untar 解包到目录
func Untar(srcFile, dstDir string) error {
	f, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		path := filepath.Join(dstDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}
