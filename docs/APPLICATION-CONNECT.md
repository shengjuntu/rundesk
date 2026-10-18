# 应用注册与集中配置

管理员使用 `POST /api/v1/applications/{appId}/connect` 登记业务应用，随后配置该应用的模型、认证、Skills、MCP 和运行环境。首次注册需要管理员身份；应用运行凭据不能创建实例或安装工具。持久保存 installationId，重新连接复用原身份。

## 初始配置与管理员修改

POST /api/v1/applications/{appId}/connect 接收 name、description、installationId、entryUrl、可选旧 instanceId/workspaceId，以及 defaults。

- defaults.skills 是“技能目录名 → 相对路径 → 文本内容”，保留 SKILL.md、脚本及模板目录。二进制技能资产继续用现有 ZIP 导入功能。
- defaults.mcp 是命名的 MCP 配置。应用只提交自己的业务工具，不需要知道其它工具。
- defaults.model 是可选初始模型名；省略时采用通用助手的默认模型名。模型提供方和认证仍由管理员配置。
- 身份先持久化再创建资源，网络超时或重启后重试复用原身份。
- 旧实例和工作区直接接管绑定，不迁移会话；同一 appId 不允许另一 installationId 接管。
- 初始安装仅补缺失项，成功登记后不再重放 defaults。管理员修改、关闭或删除的能力不会在重连时被恢复。
- 应用地址变化返回 needs_attention，要求在 RunDesk 核对入口和 MCP；不会以“自动修复”为名覆盖管理员地址。
- 请求成功返回 application、status、message、managementPath。connected 表示登记/初始配置完成，不代表模型认证、真实 MCP 网络或业务执行已验收。

保留应用自己的安装数据目录；installationId 用于区分安装，不能每次启动重新生成。

## 选用已有能力

先在通用助手的实例范围安装 Skills / 配置 MCP，可保持关闭。在应用详情点击“选用已有能力”，选中后只对该应用启用，通用助手状态不变。完整技能目录被复制；已有同名技能内容和 MCP 参数保留，仅按管理员的明确操作启用。

该列表是现有通用助手配置的复用入口，不是独立插件市场。MCP 凭据不出现在列表响应中。OAuth、环境变量、可执行程序及容器依赖仍需在目标环境有效；复制配置不等于复制所有外部依赖。Docker 镜像与构建继续在 RunDesk 运行环境中管理，本次注册接口不自动执行 Dockerfile。

## 默认协作

已登记应用自动出现在协作能力目录，通用助手自动作为默认负责人。新建协作只需填写目标；负责人、参与范围、通信方式和轮次预算折叠在高级设置中。无 Gitea 时默认直接协作。

使用有界协作执行器。普通对话不会自动转换为协作任务；从“协作”入口发起。当前新闻研究工具需要已有 run_id，视频工具需要 projectId；能力说明会明确这些前提。自动登记不是自动创建任意业务对象，也不是自动跨容器传送文件。


## 应用主动准备运行环境

0.47.0 新增 `application-runtime` 能力。普通应用通过 `GET /api/v1/applications/{appId}/runtime` 读取缓存摘要，通过 `POST /api/v1/applications/{appId}/runtime/prepare` 启动或复用管理员已经配置的 Codex Docker 环境。请求必须带预期的 instanceId/workspaceId，probe=true 可检查认证元数据及 MCP 工具发现，不运行模型。

GET 需要 read，POST 需要 read/run，且 appId、实例与工作区必须匹配凭据和当前应用绑定。接口不选择镜像、不构建、不更改工具、不删除容器；待部署配置返回冲突。完整契约见 [API v1](API-V1.md)。

0.49.3 增加 `application-runtime-diagnostics`：在已运行的绑定容器内检查实际工具版本，可验证本应用会话目录中的双向文件挑战，以及固定只读 MCP 测试图的实际 HTTP 传输。该接口不启动环境、不回退宿主机、不调用模型，`modelVisionVerified` 始终 false；完整请求、错误码与验证责任见 [诊断契约](API-V1.md#已选容器诊断0493)。

配套 news2douyin 0.13.0 在开始研究前使用此接口。首次 Docker 接入先登记应用，再选定镜像并准备环境，最后将业务 MCP 与 Skill 装到该环境；模型认证由管理员完成。部署入口见 [news2douyin 配置](../examples/news2douyin/README.md)。
