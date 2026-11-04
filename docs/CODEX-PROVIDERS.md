# Codex 模型服务 / Codex model providers

0.62.0 将 Codex 模型服务与 Kun 维护模型分别配置。初始化发现的 `/v1/models` 和 Chat Completions 测试是 Kun 接入证据；它们不能证明 Docker 里的 Codex 已切换提供方或支持其所需的 Responses 协议。

## 配置范围

- **默认 Codex 服务**：保存基础地址、模型 ID、可选密钥环境变量名和配置 revision，供新建 Agent 时显式选择继承。
- **具体 Agent／工作区**：Docker 的 `scope: environment` 保存到该环境的持久 `CODEX_HOME`；本机实例的 `scope: instance` 使用该实例共享的原生 HOME，配置会影响它在其他工作区的新轮次。模型地址与认证不写进镜像。
- **已有环境**：调整默认值不自动覆盖它们。应用项目创建时的配置快照也不会自动随源 Agent 更新，详见[应用项目环境](APPLICATION-PROJECT-ENVIRONMENTS.md)。

提供方更新保留 TOML 中其他配置值，包括 MCP 与权限；重新序列化会规范化文件格式和注释，不应将注释当作业务配置。

默认和环境修改均使用读取到的 revision 防止并发覆盖。创建 Agent 选择继承时绑定本次读取的默认 revision，重试仍使用原输入；默认配置变化时应重新审阅，而不是静默采用另一份设置。

## Docker 地址与检查

容器内的 `127.0.0.1` 指向容器本身。同机模型服务可使用容器能访问的宿主地址；RunDesk bridge 环境提供 `host.docker.internal` 的 host-gateway 映射，但服务仍须监听容器能够到达的接口。仅能从宿主 loopback 访问的服务，不能据此声明容器也能访问。RunDesk 不静默改写已填写的地址。

提供方状态分为 `unmanaged`（未由此功能管理）、`pending`（等待镜像初始化后合并）、`configured` 和 `recreate_required`（现有容器需要显式重建以更新凭据）。首次环境初始化先保留镜像 Seed 的 MCP 等配置，再合并选定的提供方。

保存配置与运行验证分开。具体环境的主动测试使用该环境的网络位置，测试 Responses 推理及无副作用的工具调用；不会因为宿主目录接口返回模型名就标记为通过。未运行的容器需要先由用户启动，测试本身不隐式启动容器。测试结果保留作用范围、时间与失败原因，不是永久就绪保证，也不是业务任务验收。

密钥只填写服务进程中的环境变量名称；接口不返回密钥值。Docker 使用明确选择的配置传递所需凭据，不继承整个宿主环境。修改服务进程环境变量后应重启 RunDesk；已有容器可能需要在空闲时显式重建才能采用新凭据，再重新检查具体运行环境。不要把密钥写入 Dockerfile、配方 ZIP、镜像或公共下载目录。

## API

以下均为管理员接口；应用和普通成员不能修改全局模型服务或其他实例的提供方。

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/setup/codex-provider` | 读取默认 Codex 配置 |
| `PUT /api/v1/setup/codex-provider` | 按 revision 保存默认基础地址、模型和密钥变量引用 |
| `GET /api/v1/instances/{iid}/codex-provider?workspaceId=...` | 读取准确实例／工作区的配置 |
| `PUT /api/v1/instances/{iid}/codex-provider?workspaceId=...` | 保存该运行环境的配置 |
| `POST /api/v1/instances/{iid}/codex-provider/test?workspaceId=...` | 在实际 host／container 范围主动检查 Responses 与工具调用 |

`PUT` 与主动测试请求使用 `Idempotency-Key`，同一逻辑重试保留原键与原请求。配置输入包含 `baseUrl`、`model`、`apiKeyEnv`、`revision`，协议固定为 `responses`。环境读回声明 `instanceId`、`workspaceId`、可选 `environmentId` 和 `scope`。完整请求、回执及权限以[OpenAPI](API-V1.md)为准。

构建中心创建 Agent 可传 `providerMode: "inherit-default"` 与读取时的 `providerRevision`；选择 `existing` 保留原有配置。镜像和 Agent 的创建关系见[构建工作台](BUILD-CENTER.md)。

## English summary

Codex provider settings are separate from Kun discovery and Chat Completions tests. A versioned default can be explicitly inherited when creating an Agent; exact instance/workspace settings live in that environment's persistent configuration. Existing environments are not overwritten by changing the default.

Provider checks use Responses and report the actual host or container scope. Discovery, saved configuration, protocol checks and business acceptance are separate states. Docker loopback is not host loopback; use a reachable endpoint and explicitly selected credential variable. Secrets are not built into images or returned in configuration responses.
