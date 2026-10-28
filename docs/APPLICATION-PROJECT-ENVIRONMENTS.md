# 应用项目与独立 Agent 环境（0.60.0）

VideoAPP 的用户只选视频项目和制作环境。RunDesk 依据项目 ID 与管理员批准的环境预设创建**独立工作区、容器和 Codex HOME**；同一 Agent 可服务多个项目，任务与产物仍留在各自的工作区。原固定 `instanceId/workspaceId` 的旧应用接口、预设、凭据和历史任务不迁移。

从构建镜像添加 Agent 时默认按镜像名称生成简短名字，同名自动加序号，仍可编辑。新建 Docker 容器的技术名称带 Agent 短名和环境唯一编号；已存在容器保持原名称。

## 接入顺序

先在 RunDesk 登记应用和额外 Agent 预设，完成镜像、模型认证、MCP 和 Skills 配置；给 VideoAPP 签发授权这些预设的应用凭据。额外预设列表仍不包含默认环境。创建项目之前在 `/meta` 检查 `application-project-environments`。

VideoAPP 已有项目 ID 必须是 **24 位小写十六进制**。创建项目环境是幂等的 `PUT`，同一 `appId + projectId + presetId` 重试返回原绑定；若项目后续选择另一预设，为同一项目再创建该预设的环境，旧任务仍用原绑定。默认环境在 URL 中使用 `__default__`；`source.presetId` 为空。

```http
PUT /api/v1/applications/rundesk-video-app/projects/aaaaaaaaaaaaaaaaaaaaaaaa/environments/hyperframes
Authorization: Bearer <VideoAPP 应用凭据>
Content-Type: application/json

{"name":"产品宣传片"}
```

响应包含 `appId`、`projectId`、`presetId`、`name`、`instanceId`、`workspaceId`、`created`，不包含宿主路径。`GET /applications/{appId}/projects/{projectId}/environments` 返回此项目当前凭据可用的绑定；管理员可在应用首页查看项目列表及只读技术编号。应用凭据必须有 `read + run`，且原凭据快照已授权相应预设；增加新预设仍需重新签发凭据。默认环境使用原 `workspaceIds` 授权。

新任务可以省略技术性 `instanceId/workspaceId`，但必须提供确切项目来源；RunDesk 验证项目和预设并填入保存的绑定：

```json
{
  "title": "视频初稿",
  "source": {
    "kind": "application",
    "appId": "rundesk-video-app",
    "projectId": "aaaaaaaaaaaaaaaaaaaaaaaa",
    "presetId": "hyperframes"
  },
  "input": {"text": "根据本项目素材制作初稿。"}
}
```

调用 `POST /api/v1/tasks` 仍需稳定 `Idempotency-Key`，返回 202 只表示接受排队。上传调用返回绑定的 `workspaceId`：`POST /workspaces/{workspaceId}/uploads`，并把返回的文件路径放进任务 `input.files`；下载和会话查询仍使用原接口。不同项目不能冒用彼此的工作区执行；省略 `source.projectId` 也不能在项目工作区发起任务。

## 配置和 MCP 项目身份

创建项目环境时，RunDesk 将所选 Agent **当前**环境的 `config.toml`、`auth.json` 和实例 Skills 复制到新 HOME。每个项目有自己的配置快照，后续修改原 Agent 不会自动改写已经创建的项目；涉及凭据轮换或能力更新时要对项目环境单独配置。镜像内的 Skills 与配置仍按容器启动时的 Seed 规则补充缺失文件。项目任务不会共享 Codex 的历史线程或缓存。

容器创建时由 RunDesk 通过 `VIDEO_APP_PROJECT_ID` 环境变量注入该 VideoAPP 项目编号。镜像提供的 `video_app_media` MCP 应使用 `env_vars` 继承它；管理员给源 Agent 配置 MCP URL 和 Token 即可。若源 `config.toml` 直接设置 `VIDEO_APP_PROJECT_ID = ...`，项目环境创建会报 `application_project_static_mcp`，避免复制固定项目身份。不同项目需要不同的应用侧访问权限时，还须由 VideoAPP 的 MCP 服务校验令牌与当前项目归属；RunDesk 注入项目 ID 不能替代 VideoAPP 自身的权限判断。

技术编号只对 VideoAPP 的服务端和 RunDesk 管理员可见。普通用户界面显示视频项目与制作环境。RunDesk 的 `runtime/prepare`、`runtime/diagnostics` 当前仍只针对旧固定预设绑定；项目环境首次执行由任务自动启动。请在 VideoAPP 不调用旧接口去准备项目环境。容器或认证出错时提示管理员处理，不能改用默认环境或另一个项目。

## 边界

- 不迁移旧任务；已接收任务继续关联旧资源。
- 项目环境需要 Codex Docker；Kun 与本机 Agent 不在此契约内。
- 同一应用凭据可访问其被授权预设下的项目，VideoAPP 必须再按业务用户限制可见项目。
- 复制的是创建时的配置快照，模型认证、MCP 连通性及视频质量仍需实际联调验收。
