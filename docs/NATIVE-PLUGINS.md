# 原生插件支持

RunDesk 0.52.0 提前建立 Codex 原生插件边界，保留独立 Skill、手动 MCP 配置及 Docker 资源的自由组合。原生插件负责能力打包；RunDesk 管运行、授权、任务、轨迹和镜像。不新增与原生插件重复的能力包格式，不将插件拆成多个手动 Skill/MCP 再声称已安装。

## 当前能力

配置页“插件”显示实例能力、原生配置和有效组件的来源，可上传 ZIP 只读检查包内容。Skills/MCP 中带 pluginId 的组件显示来源，并避免作为独立配置误编辑。

| 状态 | 含义 |
| --- | --- |
| configured | config/read 明确出现该插件配置 |
| enabled=true/false/null | 配置明确启用/停用，或无法确认 |
| loaded=observed | 有原生 MCP runtime connected/starting 的组件证据 |
| loaded=unknown | 没有足够加载证据；只发现 Skill 不证明已执行 |
| inventoryState=unknown | 当前无法通过稳定 API 核对已安装清单 |

这些状态分别表达事实。配置启用不证明已安装、加载、认证或工具可用。无 workspaceId 的查询只返回支持能力，不启动配置进程。有工作区时使用现有 `config/read`、`skills/list` 和 `mcpServerStatus/list`；缺少元数据时给出 unknown/warnings。绑定已禁用预设时不启动新执行。Kun 明确不支持 Codex 原生插件，不展开并安装其组件。

## 原生格式

只读检查接受 portable 根 `plugin.json`、`skills/`、`mcp.json`；支持 `.codex-plugin/plugin.json` 和 `.mcp.json` 兼容格式。ZIP 可有一个包根目录，拒绝歧义根、危险路径、链接、特殊项和超限输入；只在内存投影 manifest、组件、依赖与兼容提示，不执行 hooks、下载依赖或写配置。原生字段保持原格式，unknownFields 不作为安装成功依据。上限 16 MiB、解压声明 64 MiB、2048 项，JSON 各 1 MiB。

示例源包 [rundesk-build-tools](../plugins/rundesk-build-tools/README.md)包含 portable/兼容 manifest 与 `add-app-docker-image` Skill。限时 MCP 必须按实际任务动态注入，不分发带真实 URL/令牌的静态配置。构建助手当前使用明确标注的独立 Skill bootstrap，不等于原生插件安装。

## API 与后续兼容

- `GET /api/v1/instances/{iid}/plugins?workspaceId=...`：管理员只读能力、配置和组件来源。
- `POST /api/v1/plugins/inspect`：multipart `file`，只读包检查。
- 安装、卸载、清单和版本化驱动边界已预留；未实现的原生生命周期操作返回明确 unsupported/501，不显示成功。

截至本次核对，官方 [App Server 文档](https://developers.openai.com/codex/app-server)仍将 `plugin/list`、`plugin/read`、`plugin/install`、`plugin/uninstall` 标为开发中，并要求生产客户端暂不调用。本版默认不调用这些接口。以后按后端、精确版本和已验证 API 能力接入 NativePluginDriver，再开放管理按钮；不能因 CLI schema 中有方法名就认定可用于生产。

包结构依据官方[插件打包文档](https://developers.openai.com/plugins/build/plugins)。hooks、marketplace 和认证能力依实际 Codex 运行端而异，不据此承诺 Kun、Docker 和所有 Codex 界面等价支持。
