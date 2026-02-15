# ikl

镜像管理工具，支持查看私有仓库镜像与标签，并在仓库之间迁移镜像（包含多架构清单）。

## 构建

```bash
go build -o ikl
```

## 使用

### 列出仓库中的镜像列表

```bash
./ikl list-images --registry ykl.io:40443 --insecure

🔍 正在连接仓库 ykl.io:40443 获取目录...
序号	镜像仓库名称 (REPOSITORY)                 
1   	google_containers/coredns                	
2   	google_containers/etcd                   	
3   	google_containers/kube-apiserver         	
4   	google_containers/kube-controller-manager	
5   	google_containers/kube-proxy             	
6   	google_containers/kube-scheduler         	
7   	google_containers/pause                  	
8   	library/golang                           	
9   	library/nginx                            	
10  	library/python                           	
11  	library/redis                            	

共找到 11 个镜像仓库。
```

### 列出某镜像的标签列表

```bash
./ikl list-tags --registry ykl.io:40443 --repo library/redis --insecure

🔍 正在获取 ykl.io:40443/library/redis 的标签列表...
📋 共找到 1 个标签，正在获取详细信息 (并发数: 10)...
序号	标签 (TAG)	架构 (ARCH)	大小 (SIZE)	创建时间 (CREATED) 
1   	7.2       	linux/amd64	42.5 MB    	2026-01-13 10:01  

./ikl list-tags --registry ykl.io:40443 --repo library/golang --insecure
🔍 正在获取 ykl.io:40443/library/golang 的标签列表...
📋 共找到 2 个标签，正在获取详细信息 (并发数: 10)...
序号	标签 (TAG)        	架构 (ARCH)                	大小 (SIZE)	创建时间 (CREATED) 
1   	1.24.12-alpine3.23	linux/amd64, linux/arm64/v8	Index      	2026-01-28 11:21  	
2   	1.25-alpine       	linux/amd64, linux/arm64/v8	Index      	2026-01-28 11:21  		
```

### 迁移镜像（支持 amd64/arm64 的 manifest list）

准备配置文件（见 `config.example.yaml`）：

#### 配置说明

```yaml
# 可选：为不同源仓库配置认证信息（仅私有仓库需要）
source_registries:
  registry.example.com:
    username: "your_user"
    password: "your_password"
    insecure: true

# 必填：目标仓库配置（格式与 source_registries 一致，当前仅支持一个目标仓库）
destination_registries:
  ykl.io:40443:
    username: "admin"
    password: "your_password"
    insecure: true
    type: "harbor" # 仓库类型，支持 "harbor"。如果是普通repo不需要填写。

# 多行镜像列表：默认拉取 amd64/arm64；未写 tag 默认 latest
image_list: |
  docker.io/rook/ceph:v1.19.0
  quay.io/cephcsi/cephcsi:v3.16.0
  docker.io/library/nginx #arch=amd64,arm64
```

配置说明：
- `image_list` 支持 `#arch=amd64,arm64` 指定架构；不写时默认迁移 amd64/arm64。
- `image_list` 中不写 tag 时默认 `latest`。
- `source_registries` 可选，仅私有源仓库需要配置账号密码。
- `destination_registries` 必填，格式与 `source_registries` 一致，当前仅支持一个目标仓库。
- `type`仓库类型，支持 "harbor"。如果是普通repo不需要填写。

命令行参数说明：
- `--config` 配置文件路径
- `--proxy` 拉镜像可能会用到代理
- `--no-proxy` 指定本地仓库不走代理

```bash
./ikl migrate --config config.yaml --proxy http://127.0.0.1:7897 --no-proxy ykl.io

API server listening at: 127.0.0.1:4919

🚀 开始执行镜像迁移任务...

源仓库列表:
  - index.docker.io (Insecure: false, 匿名)

目标仓库: ykl.io:40443 (Insecure: true)

🌐 全局代理: http://127.0.0.1:7897

🛑 排除代理 (NoProxy): ykl.io

------------------------------------------------

🎯 镜像 library/golang 指定架构: [amd64 arm64]

⏳ 正在迁移 library/golang:1.25-alpine ...

⠇    传输中 (126 MB, 5.5 MB/s) [22s] 

⠋    传输中 ( 0 B) [0s] 

   ✅ 完成

⏳ 正在迁移 library/golang:1.24.12-alpine3.23 ...

⠴    传输中 (163 MB, 8.8 MB/s) [18s] 

⠋    传输中 ( 0 B) [0s] 

   ✅ 完成

🎯 镜像 library/redis 指定架构: [amd64]

⏳ 正在迁移 library/redis:7.2 ...

⠙    传输中 (45 MB, 7.2 MB/s) [8s] 

   ✅ 完成

------------------------------------------------

🎉 任务结束。成功: 3, 失败: 0
```
