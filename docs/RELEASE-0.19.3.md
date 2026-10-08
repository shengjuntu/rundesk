# RunDesk 0.19.3 — MCP tool approval settings / MCP 工具审批设置

RunDesk previously rejected the native `mcp_servers.<id>.tools` configuration field. This patch accepts and validates per-tool `approval_mode` (auto, prompt, writes, approve) and positive-integer `output_token_limit`. It adds an explicit allow-list editor to the selected assistant/application's MCP form. There is no automatic approval based on server names such as Tavily.

此前 RunDesk 会拒绝 Codex 原生的 MCP tools 配置字段。本版允许并校验单个工具的 approval_mode 与 output_token_limit；在当前助手／应用 MCP 编辑页提供显式允许名单，不根据 Tavily 等服务名称自动放行。

## How to use / 使用

1. Open the assistant settings, or the relevant application's settings, then MCP.
2. Edit the Tavily server. Copy exact tool names from the tool list into “Always allow these tools”, one per line. Do not use the server name or a display label as a tool name.
3. Save. RunDesk writes each listed tool's native `approval_mode = "approve"`. The existing reload mechanism applies config before the next run; this does not answer an already-pending approval.
4. Remove a name and save to remove its explicit allow policy. Other per-tool settings and unrelated policies remain intact. Removing an allow entry returns to inherited/default behavior, not necessarily “always prompt”; use the full JSON editor with `approval_mode: "prompt"` for that.

进入通用助手设置或对应应用设置 → MCP → 编辑 Tavily。在“始终允许的工具（当前助手／应用）”中，每行填写一个工具列表中的准确名称，再保存。下一次运行前按原有机制重新加载；已弹出的审批仍需处理。移除名称并保存可撤回显式允许，保留其他工具策略与输出限制。撤回后使用继承／默认策略；若要每次询问，可在完整 JSON 中指定 prompt。

Example fragment to merge into the existing MCP server config (replace the example name with the actual listed tool name):

```json
{
  "tools": {
    "EXACT_TOOL_NAME": {"approval_mode": "approve"}
  }
}
```

## Limits / 边界

Requires the installed Codex version to support this native field. Managed requirements may still require approval. MCP elicitation forms, OAuth/login requests, command/network permission prompts and model-generated questions are distinct and are not automatically answered by this setting. This patch does not turn off global approvals or grant session-wide command permissions. Configuration follows existing instance/environment boundaries; native project or managed layers may override it.

需要已安装的 Codex 支持原生字段；托管策略仍可能要求审批。工具自身的 elicitation 表单、OAuth 登录、命令／网络权限和模型主动提问是不同机制，不会自动回答。不会关闭全局审批，也不授予会话级命令权限。设置沿用已有实例／环境边界，项目和托管层可能覆盖。

## Validation / 验证

Full Go suite; policy validation and instance-isolation regression tests; browser tests for save/remove, JSON/form round-trip, preserving other tools and output limits, Chinese/English and mobile layout. Linux and Windows builds (Windows cross-compilation only). Tests use the demo App Server: no real Tavily credentials, search calls, or user's installed Codex runtime were available. The actual reported prompt must be verified after deployment; if it persists, inspect the full approval request method and parameters.

全量 Go 测试、策略校验与实例隔离回归；浏览器验证保存／撤回、JSON 切换、其他策略保留、中英文和移动端。构建 Linux／Windows（Windows 仅交叉编译）。测试使用演示 App Server，未使用真实 Tavily 凭据或用户安装的 Codex，不能声称已复现并消除实际弹窗。部署后若仍出现，请核对审批完整请求的 method 与参数。

Official configuration reference: https://learn.chatgpt.com/docs/config-file/config-reference (`mcp_servers.<id>.tools.<tool>.approval_mode`).
