# 构建中心

RunDesk 0.52.0 的“构建中心”是跨应用的常驻入口。新闻搜索、媒体搜索、深度研究和视频制作可以共用配方或镜像；应用、Skills、MCP、插件、实例和任务自由组合，不要求定义工作流或应用层级。

| 资源 | 负责的内容 |
| --- | --- |
| 原生插件 | 按 Codex 格式分发 Skills、MCP 声明和资源 |
| 构建配方 | Dockerfile、上下文 ZIP、来源、原生插件引用和本地 smoke 声明 |
| 镜像 | 实际固定 Image ID、来源、引用、可用性和检查结果 |
| 实例与环境 | 模型配置、独立 HOME、工作区及现有容器生命周期 |
| 任务 | Codex 执行、进度、审批、轨迹和产物 |

## 使用

打开左侧“构建中心”，选择已有配方构建，或导入自己的 manifest 与 Docker context ZIP。内置六项为 news2douyin、people-search、video-build、HyperFrames、video-use、OpenMontage。构建在后台排队，可以查看日志、取消和重建。已有 setup/应用构建也在全局列表中显示；原有入口继续工作。

构建成功后在镜像区检查固定身份、查看引用、执行离线检查或声明的 smoke，或者创建并启动专用 Codex Docker 实例。启动环境不发起模型任务。已有应用的镜像更新仍需在其原管理界面显式操作。

“让 Codex 添加”接受用途或开源项目地址，使用现有 Codex 实例发起一个普通队列任务。它在新专用工作区读取 `add-app-docker-image` Skill，分析依赖/来源/许可证，生成可审阅的配方和 ZIP，通过受限构建 MCP 构建、读日志、修复和测试。会话和任务仍在 RunDesk 中调试。引用项目仅为编号上下文，不直接借用其工作目录。需要可用的 Codex 和模型认证。

本版助手为每次任务 bootstrap 一个独立 Skill，并在当前原生线程配置中注入构建 MCP；回执明确返回 `skillBootstrap:true`、`nativePluginInstalled:false`。原生插件源包见 [rundesk-build-tools](../plugins/rundesk-build-tools/README.md)，插件安装另见[原生插件支持](NATIVE-PLUGINS.md)。

## 动态配方

自定义配方存于 `data/build-recipes/<id>/revisions/<revision>/`，包含冻结 manifest、原始 ZIP 和摘要；无需修改 examples 或重新编译宿主。每次导入产生独立配方版本，修改内容应导入新版本。归档不删除历史输入。内置配方只读，原有 video-build 目录与旧内置 ZIP 不变。

manifest 示例：

```json
{
  "schemaVersion": 1,
  "name": "My research tools",
  "version": "1.0.0",
  "description": "Pinned tools for a research task",
  "dockerfile": "Dockerfile",
  "plugins": [],
  "smoke": {
    "command": ["python3", "-c", "from pathlib import Path; Path('/tmp/check.txt').write_text('ok')"],
    "timeoutSeconds": 60,
    "expectedFiles": ["/tmp/check.txt"]
  }
}
```

镜像须提供 Codex CLI、Python 3 及所声明命令，适配现有 Codex Docker 环境。插件引用只记录原生 ID、可选来源和版本，不是 RunDesk 自定义插件包，也不自动安装插件。ZIP 上限 32 MiB，解压后 128 MiB、4000 项；拒绝越界路径、链接、特殊文件、重复路径和常见凭据文件。不要把真实服务密钥、Codex HOME、宿主 socket 或业务数据放入镜像。

重建建立新任务，复制原始冻结上下文及其摘要，不读取已被修改的工作目录。未保留原始输入的旧历史会明确拒绝重建。输入相同可复用 Docker 层缓存；`noCache` 仅在确有需要时指定。

## 检查、启动和删除

构建与镜像检查复用原有单队列。基础检查或内置 doctor 只验证本机工具、原生 App Server 初始化及已声明的离线行为，不调用模型。未通过检查但已生成实际 Image ID 的构建也保留镜像目录，供核对和清理，不能因此视为可用。smoke 使用冻结 argv 与产物路径，在无网络、无宿主挂载、只读根目录、限时限资源的临时容器中执行。声明命令和检查范围可审阅；通过不代表外部 MCP、模型认证、TTS 音质或完整业务质量合格。没有适用 smoke 的镜像明确返回不可用。

“创建并启动实例”默认建立专用实例、工作区及 HOME；只允许显式复用匹配且空闲的独立实例，不能借用默认助手或其他应用/预设的保留资源。失败后应先核对已创建资源与原回执，不能换 requestId 盲目重复。

以下操作分别管理不同资源：

- 原应用入口删除构建记录：清理任务与日志，不删除 Docker 镜像；全局平台构建记录保留为镜像来源。
- 删除镜像：按固定 Image ID 执行非强制 Docker 删除，保留登记历史、项目与 HOME；实例、环境、活动检查及 Docker 中的停止/外部容器引用都会阻止删除。不会对移动后的同名标签误删新镜像，不使用 prune。
- 移除容器：继续使用环境管理的原生命周期入口，保留宿主工作区和 HOME。

## 管理 API

以下为 RDCA `/api/v1` 下的管理路径，普通应用凭据和成员不能访问。新建/构建/重建/启动/测试/删除镜像均使用 24 位小写十六进制 `requestId`；同一操作重试必须沿用，参数改变需新值。该字段由各资源实现持久回执，不使用通用 JSON Idempotency-Key 拦截 multipart ZIP。

| 路径 | 操作 |
| --- | --- |
| `/build-center/recipes` | GET 列表；POST multipart `manifest`、`context`、`requestId` 导入 |
| `/build-center/recipes/{rid}` | GET 明细；DELETE `?revision=N` 归档自定义项 |
| `/build-center/recipes/{rid}/build` | POST `recipeRevision`、`requestId`，可选 reference、codexVersion、noCache |
| `/build-center/builds` | GET 全局列表 |
| `/build-center/builds/{bid}` | GET 状态；平台成功记录保留为镜像来源 |
| `/build-center/builds/{bid}/log` | GET `?after=N` 增量脱敏日志 |
| `/build-center/builds/{bid}/rebuild` | POST `requestId`、可选 noCache，复制冻结输入 |
| `/build-center/builds/{bid}/cancel` | POST 取消排队/运行任务 |
| `/build-center/images`、`/{key}` | GET 按实际身份聚合的目录与明细 |
| `/build-center/images/{key}/check` | POST 核对固定镜像身份 |
| `/build-center/images/{key}` | DELETE `imageId`、`requestId` 删除实际镜像 |
| `/build-center/images/{key}/launch` | POST `name`、`requestId`，可选专用 instanceId/workspaceId |
| `/build-center/images/{key}/tests` | POST `kind:offline\|smoke`、`requestId`，返回排队任务 |
| `/build-center/tests/{id}`、`/{id}/log` | GET 检查结果和日志 |
| `/build-center/assistant-runs`、`/{id}` | POST prompt/requestId/可选 instanceId、workspaceId；GET 助手与任务绑定 |
| `/build-center/assistant-runs/{id}/revoke` | POST 撤销构建工具权限 |

唯一例外为 `POST /api/build-center/mcp`（及 v1 别名）：只接受任务短期 Bearer 能力，端点自己验证任务、会话、实例、工作区、路径和有效期。它只可导入自己工作区的配方、构建自己的/内置配方、查看或取消自己的构建、检查自己的产出；不能删除镜像、启动/改变实例、安装插件或访问其他任务。它不是通用管理 API，管理员令牌也不替代该任务能力。

Docker 助手需将 `RUNDESK_BUILD_MCP_ORIGIN` 或 `--public-url` 设置为容器可达的来源，服务也须监听可达接口。容器内 localhost 指向容器自身；禁网络实例或未提供非回环来源会明确拒绝。短期凭据只在宿主私有数据目录和线程内存中使用，不写入镜像、提示词或共享 Codex HOME。
