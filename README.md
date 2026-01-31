# ikl

镜像管理工具，支持查看私有仓库镜像与标签，并在仓库之间迁移镜像（包含多架构清单）。

## 构建

```bash
go build -o ikl
```

## 使用

### 列出仓库中的镜像列表

```bash
./ikl list-images --registry registry.example.com --username user --password pass
```

### 列出某镜像的标签列表

```bash
./ikl list-tags --repository registry.example.com/team/app --username user --password pass
```

### 迁移镜像（支持 amd64/arm64 的 manifest list）

准备配置文件（见 `config.example.json`）：

```bash
./ikl migrate --config config.example.json
```
