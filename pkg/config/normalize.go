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

// ResolveImages merges image_list and images, applying override rules.
// image_list entries default to amd64/arm64 unless overridden by #arch= directive.
func (cfg *MigrateConfig) ResolveImages() ([]ImageEntry, error) {
	normalizedImages, err := normalizeExplicitImages(cfg.Images)
	if err != nil {
		return nil, err
	}

	entriesFromList, err := parseImageList(cfg.ImageList)
	if err != nil {
		return nil, err
	}

	return mergeImages(normalizedImages, entriesFromList), nil
}

func normalizeExplicitImages(images []ImageEntry) ([]ImageEntry, error) {
	normalized := make([]ImageEntry, 0, len(images))
	for _, img := range images {
		if img.Registry == "" {
			registry, repo, err := parseRepository(img.Name)
			if err != nil {
				return nil, fmt.Errorf("解析镜像名称失败: %s: %w", img.Name, err)
			}
			img.Registry = registry
			img.Name = repo
		}
		normalized = append(normalized, img)
	}
	return normalized, nil
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

func parseRepository(value string) (registry string, repo string, err error) {
	repository, err := name.NewRepository(value)
	if err != nil {
		ref, parseErr := name.ParseReference(value)
		if parseErr != nil {
			return "", "", err
		}
		repository = ref.Context()
	}

	return repository.RegistryStr(), repository.RepositoryStr(), nil
}

func mergeImages(explicitImages, listImages []ImageEntry) []ImageEntry {
	knownTags := make(map[string]struct{})
	knownRepos := make(map[string]struct{})

	for _, img := range explicitImages {
		if len(img.Tags) == 0 {
			knownRepos[repoKey(img)] = struct{}{}
			continue
		}
		for _, tag := range img.Tags {
			knownTags[tagKey(img.Registry, img.Name, tag)] = struct{}{}
		}
	}

	merged := append([]ImageEntry{}, explicitImages...)
	for _, img := range listImages {
		if _, ok := knownRepos[repoKey(img)]; ok {
			continue
		}
		duplicate := false
		for _, tag := range img.Tags {
			if _, ok := knownTags[tagKey(img.Registry, img.Name, tag)]; ok {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		for _, tag := range img.Tags {
			knownTags[tagKey(img.Registry, img.Name, tag)] = struct{}{}
		}
		merged = append(merged, img)
	}

	return merged
}

func repoKey(img ImageEntry) string {
	return fmt.Sprintf("%s/%s", img.Registry, img.Name)
}

func tagKey(registry, repo, tag string) string {
	return fmt.Sprintf("%s/%s:%s", registry, repo, tag)
}
