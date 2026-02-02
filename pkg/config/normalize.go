package config

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

const (
	archDirectivePrefix = "#arch="
)

var defaultArchitectures = []string{"amd64", "arm64"}

// ResolveImages parses image_list, applying default architectures and directives.
func (cfg *MigrateConfig) ResolveImages() ([]ImageEntry, error) {
	entriesFromList, err := parseImageList(cfg.ImageList)
	if err != nil {
		return nil, err
	}
	return entriesFromList, nil
}

func parseImageList(raw string) ([]ImageEntry, error) {
	lines := strings.Split(raw, "\n")
	results := make([]ImageEntry, 0, len(lines))

	for lineNumber, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		archs := []string{}
		if idx := strings.Index(line, archDirectivePrefix); idx >= 0 {
			archPart := strings.TrimSpace(line[idx+len(archDirectivePrefix):])
			if archPart != "" {
				archPart = strings.SplitN(archPart, " ", 2)[0]
				for _, arch := range strings.Split(archPart, ",") {
					arch = strings.TrimSpace(arch)
					if arch != "" {
						archs = append(archs, arch)
					}
				}
			}
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" {
			continue
		}

		ref, err := name.ParseReference(line)
		if err != nil {
			return nil, fmt.Errorf("解析 image_list 第 %d 行失败: %w", lineNumber+1, err)
		}

		if len(archs) == 0 {
			archs = append([]string{}, defaultArchitectures...)
		}

		repo := ref.Context()
		results = append(results, ImageEntry{
			Registry:      repo.RegistryStr(),
			Name:          repo.RepositoryStr(),
			Tags:          []string{ref.Identifier()},
			Architectures: archs,
		})
	}

	return results, nil
}
