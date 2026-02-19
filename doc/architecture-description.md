**ikl** 是一个使用 Go 语言编写的轻量级容器镜像管理命令行工具（CLI）。它主要用于私有镜像仓库（如 Harbor、Docker Registry）的镜像查看、标签检索、**多架构镜像在线迁移**以及**离线导出与导入**。

以下是该项目的详细技术实现细节分析：

### 1. 技术栈与核心依赖

* **语言**: Go (Go 1.25)
* **核心库**: `github.com/google/go-containerregistry`。这是 Google 官方提供的处理 OCI（Open Container Initiative）镜像标准的库，也是该工具底层与镜像仓库交互的核心。
* **CLI 框架**: `github.com/spf13/cobra`，用于构建命令行应用的子命令（migrate, list-images, save, load 等）和参数解析。
* **UI/交互**:
    * `github.com/olekukonko/tablewriter`: 终端表格渲染。
    * `github.com/schollz/progressbar/v3`: 迁移与导出导入时的进度条显示。

### 2. 核心架构模块

项目采用了典型的分层架构：

* **cmd/**: 命令行入口与业务逻辑编排，包含 `migrate`, `save`, `load`, `list` 等核心子命令。
* **pkg/registry/**: 核心模块。
    * `client.go`: 通用 OCI 仓库客户端封装，处理镜像拉取、推送、清单解析及多架构过滤。
    * `layout.go`: **OCI Image Layout** 支持，负责镜像的本地持久化存储（Save/Load）及离线包的打包/解包（Tar/Gzip）。
* **pkg/harbor/**: 针对 Harbor 仓库的特定 API 封装（如自动创建项目）。
* **pkg/config/**: 配置文件解析、数据标准化及镜像列表指令解析。
* **pkg/ui/**: 封装终端交互组件。

---

### 3. 关键功能实现细节

#### A. 镜像迁移与多架构支持 (Core Feature)

在 `pkg/registry/client.go` 中实现。

1. **Manifest List (Index) 的处理**:
    * 工具能够感知 **Manifest List**（多架构镜像索引）。
    * **架构过滤 (`filteredIndex`)**: 实现了一个自定义装饰器 `filteredIndex`。当用户指定架构（如 `linux/amd64`）时，工具会筛选原始 Index 中的 Manifest 描述符，重新构造一个仅包含选中架构的 Index 推送到目标。
2. **流式传输**: 利用 `remote.Write` 和 `remote.WriteIndex`，Blob 数据通常通过流式传输直接从源 Pipe 到目标，避免内存爆涨。

#### B. 离线导出与导入 (Offline Support)

这是针对内网/隔离环境设计的核心功能，主要在 `pkg/registry/layout.go` 实现。

1. **OCI Image Layout 存储**:
    * **导出 (`save`)**: 工具将远程镜像拉取并写入符合 OCI 标准的本地目录结构（OCI Layout）。
    * **元数据保留**: 在写入 Layout 时，通过 **OCI Annotations**（如 `ikl.original.repo`, `ikl.original.tag`）记录镜像的原始名称和标签。这使得在执行 `load` 导入时，即使离线包内只有 Digest，也能还原出原始的镜像路径。
    * **多架构支持**: 导出时同样支持架构筛选，减少离线包体积。
2. **离线包封装 (Bundle)**:
    * 使用 Tar + Gzip 将 OCI Layout 目录打包成单文件，方便分发。
3. **自动化导入 (`load`)**:
    * 导入时自动解压并读取 Layout 中的索引信息。
    * 结合配置文件中的 `TargetName` 映射，支持在导入过程中对镜像进行重命名。

#### C. 仓库交互与 Harbor 集成

1. **通用 V2 适配**: 支持所有符合 Docker Registry V2 标准的仓库，提供 `Insecure` 模式支持内网自签名证书。
2. **代理与直连**: 实现了灵活的全局 `HTTP_PROXY` 支持，并能根据 `NoProxy` 列表自动判断内网仓库是否跳过代理。
3. **Harbor 自动化**: 识别目标为 Harbor 时，推送前会自动检查并调用 API 创建不存在的项目（Project），并支持 HTTPS 到 HTTP 的自动降级。

#### D. 配置系统与指令解析

* **行内指令**: 支持在镜像列表字符串中使用 `#arch=amd64` 等后缀指令，实现精细化的迁移策略控制。

---

### 4. 代码亮点与设计模式

1. **Decorator (装饰器模式)**: `filteredIndex` 完美包装了 `v1.ImageIndex`，在不改变原有库接口的前提下实现了架构过滤。
2. **Semaphore (信号量)**: 在标签列表查询等高并发场景，使用 Channel 信号量精准控制并发数（Worker Pool），平衡性能与服务端负载。
3. **Fallback 机制**: 针对内网复杂的网络协议环境，实现了连接协议的自动降级重试。

### 总结

**ikl** 是一个专注于**特定痛点**（私有仓库间迁移、多架构筛选、离线环境部署、Harbor 项目自动管理）的工程化工具。它基于标准的 OCI 规范进行设计，通过 OCI Layout 解决了镜像跨网迁移的可靠性问题，是一个典型的“小而美”的 DevOps 工具。