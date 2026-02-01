package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// RegistryConfig 定义单个仓库的连接信息
type RegistryConfig struct {
	Registry string `yaml:"registry"` // 仓库地址，如 localhost:5000 或 registry.example.com
	Username string `yaml:"username"` // 用户名
	Password string `yaml:"password"` // 密码
	Insecure bool   `yaml:"insecure"` // 是否跳过 TLS 验证或使用 HTTP
}

// ImageEntry 定义要迁移的镜像条目
type ImageEntry struct {
	Name          string   `yaml:"name"`          // 镜像名称，如 library/nginx
	Tags          []string `yaml:"tags"`          // 需要迁移的 Tag 列表，为空则迁移所有
	Architectures []string `yaml:"architectures"` // 架构筛选，如 [amd64, arm64]，为空则迁移所有
}

// MigrateConfig 对应整个 config.yaml 文件的结构
type MigrateConfig struct {
	Source      RegistryConfig `yaml:"source"`      // 源仓库
	Destination RegistryConfig `yaml:"destination"` // 目标仓库
	Images      []ImageEntry   `yaml:"images"`      // 镜像列表
}

// LoadConfig 读取并解析 YAML 配置文件
func LoadConfig(path string) (*MigrateConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg MigrateConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
