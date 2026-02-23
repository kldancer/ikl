# Imt

镜像管理工具，支持查看私有仓库镜像与标签，并在仓库之间迁移镜像（包含多架构清单）。

## 构建

```bash
go build -o Imt main.go
```

## 使用

### 列出仓库中的镜像列表

```bash
$ ./Imt list-images --registry ykl.io:40443 --insecure

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
$ ./Imt list-tags --registry ykl.io:40443 --repo library/redis --insecure

🔍 正在获取 ykl.io:40443/library/redis 的标签列表...
📋 共找到 1 个标签，正在获取详细信息 (并发数: 10)...
序号	标签 (TAG)	架构 (ARCH)	大小 (SIZE)	创建时间 (CREATED) 
1   	7.2       	linux/amd64	42.5 MB    	2026-01-13 10:01  

./Imt list-tags --registry ykl.io:40443 --repo library/golang --insecure
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
$ ./Imt migrate --config config.yaml --proxy http://127.0.0.1:7897 --no-proxy ykl.io

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


**注： 在实际生产环境中，harbor镜像仓库的环境通常无法访外网，所以通常需要准备离线镜像包用来导入到私有镜像仓库中。此时可以使用以下两个功能: **

### 导出镜像
从源仓库导出镜像并保存为离线 OCI Image Bundle。支持 **断点续传**：通过 `--workspace` 指定一个持久化目录，任务失败后重新运行可跳过已完成的镜像。

config-save.yaml
```yaml
image_list: |
  registry.aliyuncs.com/google_containers/kube-apiserver:v1.34.4
  registry.aliyuncs.com/google_containers/kube-controller-manager:v1.34.4
  registry.aliyuncs.com/google_containers/kube-scheduler:v1.34.4
  registry.aliyuncs.com/google_containers/kube-proxy:v1.34.4
  registry.aliyuncs.com/google_containers/coredns:v1.12.1
  registry.aliyuncs.com/google_containers/pause:3.10.1
  registry.aliyuncs.com/google_containers/etcd:3.6.5-0
  docker.io/kubeovn/kube-ovn:v1.15.2
  docker.io/kubeovn/vpc-nat-gateway:v1.15.2
  ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick
  registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:v1.35.0
  docker.io/projecthami/hami:v2.7.1
  docker.io/jettech/kube-webhook-certgen:v1.5.2
  docker.io/liangjw/kube-webhook-certgen:v1.1.1
  docker.io/projecthami/hami-webui-fe-oss:v1.0.5
  docker.io/projecthami/hami-webui-be-oss:v1.0.5
  nvcr.io/nvidia/k8s/dcgm-exporter:3.3.7-3.5.0-ubuntu22.04
```
执行结果：
```bash
$ ./Imt save --config config-save.yaml --output my-bundle.tar --proxy http://127.0.0.1:7897 --workspace ./my-save-work
🌐 全局代理: http://127.0.0.1:7897
📦 开始导出 17 个镜像到离线包...
[1/17] 正在导出 registry.aliyuncs.com/google_containers/kube-apiserver:v1.34.4 ([amd64 arm64])...
[2/17] 正在导出 registry.aliyuncs.com/google_containers/kube-controller-manager:v1.34.4 ([amd64 arm64])...
[3/17] 正在导出 registry.aliyuncs.com/google_containers/kube-scheduler:v1.34.4 ([amd64 arm64])...
[4/17] 正在导出 registry.aliyuncs.com/google_containers/kube-proxy:v1.34.4 ([amd64 arm64])...
[5/17] 正在导出 registry.aliyuncs.com/google_containers/coredns:v1.12.1 ([amd64 arm64])...
[6/17] 正在导出 registry.aliyuncs.com/google_containers/pause:3.10.1 ([amd64 arm64])...
[7/17] 正在导出 registry.aliyuncs.com/google_containers/etcd:3.6.5-0 ([amd64 arm64])...
[8/17] 正在导出 index.docker.io/kubeovn/kube-ovn:v1.15.2 ([amd64 arm64])...
[9/17] 正在导出 index.docker.io/kubeovn/vpc-nat-gateway:v1.15.2 ([amd64 arm64])...
[10/17] 正在导出 ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick ([amd64 arm64])...
[11/17] 正在导出 registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:v1.35.0 ([amd64 arm64])...
[12/17] 正在导出 index.docker.io/projecthami/hami:v2.7.1 ([amd64 arm64])...
[13/17] 正在导出 index.docker.io/jettech/kube-webhook-certgen:v1.5.2 ([amd64 arm64])...
[14/17] 正在导出 index.docker.io/liangjw/kube-webhook-certgen:v1.1.1 ([amd64 arm64])...
[15/17] 正在导出 index.docker.io/projecthami/hami-webui-fe-oss:v1.0.5 ([amd64 arm64])...
[16/17] 正在导出 index.docker.io/projecthami/hami-webui-be-oss:v1.0.5 ([amd64 arm64])...
[17/17] 正在导出 nvcr.io/nvidia/k8s/dcgm-exporter:3.3.7-3.5.0-ubuntu22.04 ([amd64 arm64])...
🗜️  正在打包为 my-bundle.tar...
✅ 离线包已导出至: my-bundle.tar

```

### 导入镜像
从离线 OCI Image Bundle 导入镜像到目标仓库

config-load.yaml
```yaml
destination_registries:
  ykl.io:8080:
    username: "admin"
    password: "Harbor12345"
    insecure: true
    type: harbor
```
执行结果
```bash
$ ./Imt load --config config-load.yaml --input my-bundle.tar 
📂 正在解压离线包 my-bundle.tar...
🚀 准备从离线包导入 17 个镜像到 ykl.io:8080...
[1/17] 正在导入 google_containers/kube-apiserver:v1.34.4 -> google_containers/kube-apiserver:v1.34.4 ...
✨ 目标 Harbor 项目 'google_containers' 不存在，正在自动创建...
⠴    传输中 (51 MB, 88 MB/s) [0s]   

   ✅ 完成
[2/17] 正在导入 google_containers/kube-controller-manager:v1.34.4 -> google_containers/kube-controller-manager:v1.34.4 ...
⠦    传输中 (43 MB, 70 MB/s) [0s]   

   ✅ 完成
[3/17] 正在导入 google_containers/kube-scheduler:v1.34.4 -> google_containers/kube-scheduler:v1.34.4 ...
⠦    传输中 (32 MB, 54 MB/s) [0s]   

   ✅ 完成
[4/17] 正在导入 google_containers/kube-proxy:v1.34.4 -> google_containers/kube-proxy:v1.34.4 ...
⠸    传输中 (49 MB, 148 MB/s) [0s] 

   ✅ 完成
[5/17] 正在导入 google_containers/coredns:v1.12.1 -> google_containers/coredns:v1.12.1 ...
⠴    传输中 (42 MB, 80 MB/s) [0s]   

   ✅ 完成
[6/17] 正在导入 google_containers/pause:3.10.1 -> google_containers/pause:3.10.1 ...
⠋    传输中 (572 MB, 552 MB/s) [1s] 

   ✅ 完成
[7/17] 正在导入 google_containers/etcd:3.6.5-0 -> google_containers/etcd:3.6.5-0 ...
⠦    传输中 (43 MB, 69 MB/s) [0s]   

   ✅ 完成
[8/17] 正在导入 kubeovn/kube-ovn:v1.15.2 -> kubeovn/kube-ovn:v1.15.2 ...
✨ 目标 Harbor 项目 'kubeovn' 不存在，正在自动创建...
⠇    传输中 (483 MB, 564 MB/s) [0s] 

   ✅ 完成
[9/17] 正在导入 kubeovn/vpc-nat-gateway:v1.15.2 -> kubeovn/vpc-nat-gateway:v1.15.2 ...
⠸    传输中 (21 MB, 60 MB/s) [0s]  

   ✅ 完成
[10/17] 正在导入 k8snetworkplumbingwg/multus-cni:snapshot-thick -> k8snetworkplumbingwg/multus-cni:snapshot-thick ...
✨ 目标 Harbor 项目 'k8snetworkplumbingwg' 不存在，正在自动创建...
⠧    传输中 (370 MB, 509 MB/s) [0s] 

   ✅ 完成
[11/17] 正在导入 google_containers/kube-scheduler:v1.35.0 -> google_containers/kube-scheduler:v1.35.0 ...
⠼    传输中 (32 MB, 75 MB/s) [0s]   

   ✅ 完成
[12/17] 正在导入 projecthami/hami:v2.7.1 -> projecthami/hami:v2.7.1 ...
✨ 目标 Harbor 项目 'projecthami' 不存在，正在自动创建...
⠦    传输中 (275 MB, 451 MB/s) [0s] 

   ✅ 完成
[13/17] 正在导入 jettech/kube-webhook-certgen:v1.5.2 -> jettech/kube-webhook-certgen:v1.5.2 ...
✨ 目标 Harbor 项目 'jettech' 不存在，正在自动创建...
⠹    传输中 (16 MB, 60 MB/s) [0s]  

   ✅ 完成
[14/17] 正在导入 liangjw/kube-webhook-certgen:v1.1.1 -> liangjw/kube-webhook-certgen:v1.1.1 ...
✨ 目标 Harbor 项目 'liangjw' 不存在，正在自动创建...
⠸    传输中 (36 MB, 119 MB/s) [0s] 

   ✅ 完成
[15/17] 正在导入 projecthami/hami-webui-fe-oss:v1.0.5 -> projecthami/hami-webui-fe-oss:v1.0.5 ...
⠦    传输中 (332 MB, 511 MB/s) [0s] 

   ✅ 完成
[16/17] 正在导入 projecthami/hami-webui-be-oss:v1.0.5 -> projecthami/hami-webui-be-oss:v1.0.5 ...
⠸    传输中 (127 MB, 347 MB/s) [0s] 

   ✅ 完成
[17/17] 正在导入 nvidia/k8s/dcgm-exporter:3.3.7-3.5.0-ubuntu22.04 -> nvidia/k8s/dcgm-exporter:3.3.7-3.5.0-ubuntu22.04 ...
✨ 目标 Harbor 项目 'nvidia' 不存在，正在自动创建...
⠇    传输中 (790 MB, 437 MB/s) [1s] 

   ✅ 完成
------------------------------------------------
🎉 任务结束。成功: 17, 失败: 0

```