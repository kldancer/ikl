**ikl** 是一个使用 Go 语言编写的轻量级容器镜像管理命令行工具（CLI）。它主要用于私有镜像仓库（如 Harbor、Docker Registry）的镜像查看、标签检索以及**多架构镜像迁移**。

以下是该项目的详细技术实现细节分析：

### 1. 技术栈与核心依赖

* **语言**: Go (Go 1.25)
* **核心库**: `github.com/google/go-containerregistry`。这是 Google 官方提供的处理 OCI（Open Container Initiative）镜像标准的库，也是该工具底层与镜像仓库交互的核心。
* **CLI 框架**: `github.com/spf13/cobra`，用于构建命令行应用的子命令（migrate, list-images 等）和参数解析。
* **UI/交互**:
* `github.com/olekukonko/tablewriter`: 终端表格渲染。
* `github.com/schollz/progressbar/v3`: 迁移时的进度条显示。



### 2. 核心架构模块

项目采用了典型的分层架构：

* **cmd/**: 命令行入口与业务逻辑编排。
* **pkg/registry/**: 通用 OCI 仓库客户端封装，处理底层的镜像拉取、推送和清单解析。
* **pkg/harbor/**: 针对 Harbor 仓库的特定 API 封装（非 OCI 标准部分，如项目管理）。
* **pkg/config/**: 配置文件解析与数据标准化。

---

### 3. 关键功能实现细节

#### A. 镜像迁移与多架构支持 (Core Feature)

这是该工具最复杂的部分，主要在 `cmd/migrate.go` 和 `pkg/registry/client.go` 中实现。

1. **Manifest List (Index) 的处理**:
* 工具不仅是简单的复制 Layer，它能够感知 **Manifest List**（即多架构镜像的索引）。
* **架构过滤 (`filteredIndex`)**: 在 `pkg/registry/client.go` 的 `CopyImage` 函数中，实现了一个自定义结构体 `filteredIndex`。
* 当源镜像是多架构（Index）且用户指定了特定架构（如只迁移 `linux/amd64`）时，工具不会盲目复制整个 Index。
* 它会解析原始 Index，筛选出符合 `platforms` 要求的 Manifest 描述符。
* 然后重新构造一个新的 Index（仅包含被选中的架构）推送到目标仓库。
* **代码引用**: `pkg/registry/client.go` 中的 `filteredIndex` 结构体及其 `IndexManifest` 方法。




2. **流式传输**:
* 利用 `remote.Write` 和 `remote.WriteIndex`，工具在内存中处理 Manifest，但 Blob（实际镜像层数据）通常通过流式传输直接从源 Pipe 到目标，或者根据库的实现进行高效传输，避免占用过大内存。


3. **并发进度条**:
* 使用 Go Channel (`chan v1.Update`) 将底层的传输进度实时反馈到 UI 层。



#### B. 仓库交互 (Registry Client)

位于 `pkg/registry/client.go`。

1. **通用适配**:
* 通过 `name.NewRegistry` 和 `remote.Catalog` 实现对所有符合 Docker V2 API 标准的仓库的支持。
* **Insecure 模式**: 支持跳过 TLS 验证 (`InsecureSkipVerify: true`)，这对于内网自签名的私有仓库非常重要。
* **代理支持**: 自定义了 `http.Transport`，实现了对 `HTTP_PROXY` 的支持，并且通过 `NoProxy` 逻辑实现了对特定域名的直连（代码中通过逗号分隔的字符串手动解析判断）。


2. **并发获取标签详情**:
* 在 `cmd/list.go` (`list-tags` 命令) 中，为了提高性能，工具使用了 **Worker Pool** 模式。
* 获取 Tag 列表后，开启了并发数限制（代码中硬编码为 `sem <- struct{}{}` 限制并发数为 10）的 Goroutines 去并发请求每个 Tag 的详细信息（大小、架构、创建时间）。



#### C. Harbor 特性集成

位于 `pkg/harbor/client.go`。

* **自动创建项目**: 标准 OCI API 只能推镜像，不能创建项目（Project/Namespace）。
* 该工具识别出目标仓库类型为 `harbor` 时，会额外初始化一个 Harbor Client。
* 在推送镜像前，它会调用 Harbor 的 REST API (`/api/v2.0/projects`) 检查项目是否存在。如果不存在，则自动创建私有项目。
* **协议降级 (Fallback)**: 代码中包含了一个有趣的鲁棒性设计。如果配置使用 HTTPS 连接 Harbor，但服务端返回 "server gave HTTP response to HTTPS client"，它会自动降级为 HTTP 重试。

#### D. 配置系统

位于 `pkg/config/`。

* **混合配置解析**: 配置文件不仅是标准的 YAML，其 `image_list` 字段是一个多行字符串。
* **行内指令解析**: 在 `normalize.go` 中，工具手动解析每一行镜像字符串，支持行尾注释样式的指令 `#arch=amd64,arm64`。这允许用户为列表中的不同镜像指定不同的迁移架构策略。

### 4. 代码亮点与设计模式

1. **Decorator / Wrapper 模式**:
* `filteredIndex` 结构体包装了原始的 `v1.ImageIndex`，重写了 `IndexManifest` 方法来改变行为（过滤架构），这是典型的装饰器模式应用，非常优雅地利用了接口多态性。


2. **Semaphore (信号量)模式**:
* 在 `cmd/list.go` 中使用 `chan struct{}` 作为信号量来控制并发请求数量，防止对仓库服务端造成过大压力（DDoS）。


```go
sem := make(chan struct{}, 10) // 限制并发数为 10
// ...
sem <- struct{}{}
defer func() { <-sem }()

```


3. **Cobra 命令行结构**:
* 利用 `init()` 函数和全局变量 (`rootCmd`, `proxy`, `noProxy`) 注册 Flag，使得参数可以在子命令间共享（如代理设置）。



### 总结

**ikl** 是一个专注于**特定痛点**（私有仓库间迁移、多架构筛选、Harbor 项目自动创建）的工具。它没有试图重新发明轮子，而是基于强大的 Google Container Registry 库进行了业务逻辑封装，代码结构清晰，针对内网环境（代理、自签名证书）做了很多适配工作。