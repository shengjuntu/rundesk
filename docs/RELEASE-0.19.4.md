# RunDesk 0.19.4 — Choose MCP tool permissions / 从列表选择工具权限

The MCP editor now shows each discovered or already-configured tool with a permission selector. Manual tool-name entry is no longer required for the normal workflow. Existing native approval-mode support from 0.19.3 is retained.

MCP 编辑页改为逐项列出已发现或已有配置的工具，并提供权限选择；正常操作无需手填工具名称，保留 0.19.3 的原生审批配置支持。

## Workflow / 操作

Assistant/application settings → MCP → edit the server → Load tools from saved server → choose permissions → Save settings. Save new connection details before loading. Discovery connects to the saved server and sends tools/list, never tools/call. Paginated results can be loaded with Next page. Existing configured tools remain editable when discovery is unavailable and are labelled as unverified.

助手／应用设置 → MCP → 编辑服务 → 读取已保存服务的工具列表 → 逐项选择 → 保存配置。新连接信息先保存。读取列表只连接并调用 tools/list，不执行工具；支持分页。服务不可连接时，已有工具配置仍可编辑，并标明尚未确认可用。

| Choice / 选项 | Effect / 效果 |
|---|---|
| Enabled · default approvals / 启用 · 按默认审批 | Remove explicit per-tool approval override and enable in any existing allow-list / 移除单工具审批覆盖并恢复可用 |
| Always allow / 始终允许 | Set native approval_mode=approve / 原生允许策略 |
| Ask every time / 每次询问 | Set native approval_mode=prompt / 原生询问策略 |
| Disable tool / 禁用工具 | Add to disabled_tools; preserve its policy for inspection / 加入禁用列表，保留原审批配置 |

Only edited rows change. Existing auto/writes policies are preserved and displayed. Tool output limits, other tools, server enabled state and connection secrets are not changed by permission selection. An existing enabled_tools allow-list gains only tools explicitly enabled by the user. Switching JSON/form mode preserves edited permissions. Returning to default does not necessarily mean prompting: inherited policy decides.

只修改用户编辑的行，保留已有 auto/writes 策略、输出限制、其他工具配置、服务启停与连接凭据。若已有 enabled_tools 白名单，仅将显式启用的工具加入。JSON／表单切换保留更改。“默认”由继承策略决定，不等同于每次询问。

## Boundaries / 边界

Native approval configuration requires a compatible Codex version and can be constrained by managed policies. Pending approvals, MCP elicitation forms and sign-in requests are not automatically answered. Tool discovery reuses the existing independent MCP client; local stdio and Streamable HTTP are supported, while direct Docker/OAuth discovery remains unsupported. An available Codex status catalog can populate the list without a direct connection. Failed discovery never changes policies or grants broad permissions.

需要 Codex 支持原生审批字段，托管策略仍可能要求审批。不会自动回答已弹出的审批、工具表单或登录请求。列表读取复用现有独立 MCP 客户端，支持本机 stdio 与 Streamable HTTP；直接 Docker／OAuth 读取尚未支持。已有 Codex 状态目录也可填充列表。读取失败不会改变策略或扩大授权。

## Validation / 验证

Full Go suite and a browser scenario with a local HTTP MCP fixture: two-page discovery, zero tools/call, all four choices, allow-list updates, JSON/form round-trip, preservation of unrelated policies/output limits, and Chinese/English mobile layout. Linux and Windows binaries built; Windows cross-compiled only. No live Tavily credentials or real-model approval behavior tested.

全量 Go 测试；本机 HTTP MCP 浏览器验证两页工具列表、零工具执行、四种选择、白名单更新、JSON 切换、其他策略保留及中英文移动端。构建 Linux／Windows，Windows 仅交叉编译。未使用真实 Tavily 凭据或验证真实模型审批行为。
