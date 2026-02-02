package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// RegistryConfig 定义单个仓库的连接信息
type RegistryConfig struct {
	Registry string `yaml:"registry"` // 仓库地址
	Username string `yaml:"username"` // 用户名
	Password string `yaml:"password"` // 密码
	Insecure bool   `yaml:"insecure"` // 是否跳过 TLS 验证
	Type     string `yaml:"type"`     // [新增] 仓库类型，例如 "harbor"
}

// ImageEntry 定义要迁移的镜像条目
type ImageEntry struct {
	Name          string   `yaml:"name"`          // 源镜像名称
	TargetName    string   `yaml:"target_name"`   // 目标镜像名称
	Tags          []string `yaml:"tags"`          // Tag 列表
	Architectures []string `yaml:"architectures"` // 架构筛选
}

// MigrateConfig 对应整个 config.yaml 文件的结构
type MigrateConfig struct {
	Source      RegistryConfig `yaml:"source"`      // 源仓库
	Destination RegistryConfig `yaml:"destination"` // 目标仓库
	Images      []ImageEntry   `yaml:"images"`      // 镜像列表
}

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
