# 同一应用的多个 Docker 运行预设

RunDesk 0.50.0 允许同一应用登记多个管理员批准的运行预设。应用只提交 presetId，RunDesk 解析对应实例和工作区，再复用或启动该环境。不同预设不通过修改同一个全局镜像切换，已有任务和租约保持原归属。

每个预设使用独立 Codex Docker 实例、工作区及对应环境 HOME。原 Application 的 instanceId/workspaceId 仍是旧默认绑定；不指定 presetId 的旧客户端和旧凭据保持原行为。Kun、RDCA 名称与 /api/v1 入口不变。

## 在 RunDesk 配置

在应用配置中打开“运行环境预设”，创建一个有名称的预设，例如 hyperframes、openmontage 或 video-use。可创建专用实例与工作区，或绑定符合归属要求的现有资源。填写实际可用的 Docker 镜像，再到该环境的镜像、模型认证、Skills/MCP 配置完成准备。

预设绑定的实例/工作区登记后不可替换；名称、说明和启用状态可按 revision 更新。需要另一种制作体系时建立新预设。停用保留历史记录，阻止新的执行；已接收的运行继续。停止任务仍针对原 session/run，不会切到当前默认环境。

预设名称只是管理员定义的标识，不表示镜像已构建或工具已安装。构建配方 /setup/runtime-profiles 与这里的应用运行预设是两种资源；例如 video-build 1.4.0 是镜像构建配方。OpenMontage 或自定义工具镜像需另行准备，本版不包含新的上游源码或模型权重，也不自动转换 App 剪辑工程。

已绑定实例的镜像更新沿用现有显式选择、环境差异与空闲重建机制，不在 App 提交任务时覆盖。需要保留不同版本并行工作时，为新镜像登记另一专用预设。

## RDCA 接口

所有路径均以 /api/v1 为前缀。先检查 /meta 的 application-runtime-presets 能力。

| 操作 | 路径 | 作用 |
| --- | --- | --- |
| 列表 | GET /applications/{appId}/runtime-presets | 管理员可查看；应用凭据只读其授权预设 |
| 登记/启停 | PUT /applications/{appId}/runtime-presets/{presetId} | 仅管理员；不执行模型 |
| 连接 | POST /applications/{appId}/connect | 可选 presetId，返回该预设的绑定，原默认不变 |
| 状态 | GET /applications/{appId}/runtime?presetId=... | 不启动容器，返回选中绑定 |
| 技能资源 | GET /applications/{appId}/runtime/skills?presetId=... | read；只返回已准备连接中的name/path/enabled与绑定，不开放完整配置 |
| 准备 | POST /applications/{appId}/runtime/prepare | body携带presetId和精确instanceId/workspaceId |
| 诊断 | POST /applications/{appId}/runtime/diagnostics | body携带presetId；只检查已选容器 |

presetId 为1..128个字母、数字、点、下划线或连字符，首字符必须为字母或数字。创建示例：

```json
{
  "name": "HyperFrames",
  "description": "新闻动画制作环境",
  "instanceId": "DEDICATED_CODEX_DOCKER_INSTANCE",
  "workspaceId": "DEDICATED_WORKSPACE",
  "enabled": true
}
```

更新增加当前 revision，保持 instanceId/workspaceId 原值。实例必须配置为 Codex Docker，不能接管默认助手、其他应用或其他预设的资源，也不能复用有不匹配来源历史的执行身份或重叠工作目录。

runtime响应增加 presetId；connect响应顶层增加 presetId，其中 application 的实例/工作区为本次选中绑定。空或省略 presetId 使用原默认绑定，不表示自动挑选第一个预设。

会话、队列任务与定时任务的 source 增加可选 presetId，必须与实际实例和工作区精确一致：

```json
{
  "instanceId": "DEDICATED_CODEX_DOCKER_INSTANCE",
  "workspaceId": "DEDICATED_WORKSPACE",
  "title": "视频初稿",
  "source": {"kind": "application", "appId": "rundesk-video-app", "presetId": "hyperframes"}
}
```

源任务标签和幂等键分别保留原语义。已接收或结果未确认的请求查询原回执，不换预设或新键重放。排队任务在实际启动时复核授权和预设启用状态。

## 应用凭据

KeyInput增加 presetIds。workspaceIds 继续授权旧默认实例的工作区；presetIds授权指定预设的精确 instanceId/workspaceId tuple。两者可分别为空，但不能同时为空。新增预设不会扩大旧凭据权限；需要访问时重新签发相应凭据。

ApplicationKey返回 presetBindings 快照。授权不得把实例列表和工作区列表交叉组合，也不会因同appId就允许读取未授权预设。预设appKey连接需要read，且只能确认管理员已经初始化的连接，不应用或更改defaults；准备/诊断需read+run。原默认连接与Skills/MCP初配仍由管理员处理。

## Video App 配套

Video App 0.25.0 可通过 --production-profile 或 VIDEO_PRODUCTION_PROFILE 指定新项目默认预设。创建项目的 productionProfile 可覆盖该默认；显式空字符串选择旧默认。创建请求会固化选择，首次连接再保存具体tuple；改变App启动默认不迁移旧项目或旧会话。

日常页面仍是导入、开始制作、查看视频、对话调整、导出。过程调试、镜像、认证和工具配置留在RunDesk。App自有渲染容器、IndexTTS与ComfyUI服务继续独立；选择工具环境不等于更换渲染后端。

## 验证边界

模拟Docker与协议测试验证选择、复用、权限、持久化和隔离规则，不代替实际镜像、Codex认证或制作质量验收。环境准备和只读诊断不调用模型。具体本轮检查见 validation-0.50.0.json；部署后用短任务分别核对两个镜像的实际工具和成片。
