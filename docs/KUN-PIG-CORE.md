# Kun 与 PiG 核心功能对齐

RunDesk 0.44.0 / Kun 0.16.0 / worker v16。沿用已确认方向：分进程、同仓库、选择性参考 PiG，并独立发展 Kun。七类编程动作已覆盖，PiG 的完整行为、配置、会话格式和扩展生态不作为已完成项。

代码对照基线为 [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG) 的 `827932db70e545b34c3f1d9c04f58a70eacd3b28`；逐文件来源与 MIT 许可见 [UPSTREAM](UPSTREAM.md)。原来已有两份 SSE 实现和一个选取测试的来源说明及许可，本版另在中英文 README 明确致谢。新增工具由 Kun 独立实现，没有把行为参考误记为复制代码。

## 核心工具对照

| PiG 动作 | Kun 工具 | 本版实现及边界 |
| --- | --- | --- |
| read | read_file | 工作区相对路径，UTF-8 普通文本，128 KiB；无长文件分页、图像读取 |
| write | write_file | 明确启用文件写入后覆盖或创建，128 KiB；失败不自动回滚 |
| edit | edit_file | 最多 32 个精确且唯一的替换，都对原文件匹配；重叠/歧义/缺失任一失败则不改文件；临时文件同步后替换；不支持模糊匹配 |
| ls | list_files | 单层目录，最多 500 项 |
| grep | grep | Go 正则或字面搜索、大小写选项、简单 glob、路径和行号；默认 100 / 最多 500 处匹配 |
| find | find | 递归遍历，以 basename 或工作区相对路径匹配简单 glob；不支持 `**` |
| bash | bash | 宿主 Bash 一次命令：管道、重定向、Git、编译、解释器和测试；独立授权、持久审批、超时/取消、结构化结果 |

搜索跳过 `.git`、符号链接和非普通文件；grep 另跳过二进制、非 UTF-8 和超过 1 MiB 的文件。最多访问 10,000 项、读取 16 MiB、匹配路径与文本累计 128 KiB；单行显示最多约 2 KiB。结果给出 `truncated` 和 `skippedFiles`；这些标记意味着结果不完整。这里不是 ripgrep 或 PiG 全部 glob/忽略规则的兼容实现。

## 开启与使用 Bash

1. 两个程序一起升级。在实例权限中选择完整访问，然后在 Agent 引擎配置明确启用“允许 Bash 宿主命令”。默认关闭；不自动提高已有实例权限。
2. 系统需安装 Bash。Linux/macOS 查找 `/bin/bash` 和 PATH；Windows 查找 Git for Windows 的 Bash 或 PATH。可填写 `bashPath` 绝对路径，找不到会明确报错，不冒充 PowerShell 或 sh。
3. 审批方式默认 `prompt`，每次明确允许或拒绝；管理员可选 `approve` 始终允许。实例 `approvalPolicy=never` 时，需要询问的命令直接拒绝；不会把不弹窗解释成自动批准。
4. 命令从项目目录启动，使用新建的非交互 Shell。`cd`、export 不跨调用保留；不读登录配置，移除 `BASH_ENV` / `ENV` 自动启动脚本。运行语言所需的 Python、Node、Go 等需由执行环境预先安装。

```json
{
  "allowBash": true,
  "bashApproval": "prompt",
  "bashPath": "",
  "bashTimeoutSeconds": 120
}
```

模型调用示例：

```json
{"command":"python3 'generated app.py' > result.txt; cat result.txt","timeout":30}
```

默认单次上限 120 秒，可配置到 600 秒；工具参数只能缩短该上限，仍受剩余活动预算约束。stdout/stderr 分别持续排空并保留最后 64 KiB 原始字节，结果包括 `exitCode`、`timedOut`、`cancelled`、`truncated`、原始字节计数和时长；无有效退出码时为 null。非零退出、超时和取消是工具失败，保留部分输出及可能已产生的文件变化。当前仅完成时显示结果，无交互终端、持续后台任务或流式终端输出。

生成代码的链路是：`write_file` 写入代码 → `edit_file` 修改 → `bash` 调用解释器/编译器或测试 → `read_file` / `grep` 检查结果。实际 HTTP 验收使用生成并修改的 Python 程序，输出为 `6`，不是只检查工具名称或模拟 Bash 返回值。

## 沙盒和副作用边界

PiG 官方 [Security](https://pi-in-go.dev/docs/latest/security/) 说明它以当前用户权限运行，本身不是模型或工具的 OS 沙盒；[Containerization](https://pi-in-go.dev/docs/latest/containerization/) 建议通过外部容器/虚拟机约束执行。以上为 2026-10-11 核对的滚动文档，不代表固定提交中的全部部署能力。

Kun 本版 Bash 同样使用 worker 的宿主账户权限，可访问项目外文件、环境变量与网络。`allowWrite=false` 只控制内置文件写入/编辑，不能限制 Bash 或 MCP。不要把 `os.Root` 路径限制、命令超时、人工审批或 K4 独立文件目录称为进程沙盒。

普通 Linux/macOS 子进程组在命令完成、超时或取消后清理；主动脱离进程组的后代、worker 被外部强杀的情况不保证全部清理。Windows 使用 taskkill 终止树，当前仅交叉编译验证，仍需原生测试。命令中途失败不会撤销已经产生的外部副作用。

RunDesk 现有 Docker 托管仍与 Codex 绑定，Kun 尚未接入。可在管理员自行配置的容器/虚拟机内运行整个 RunDesk/Kun，但本版未提供已验收的一键隔离配置。下一批要建立 Kun 执行环境适配：把代码工作区挂载、身份、网络、CPU/内存/进程限制和超时清理显式化，统一返回同一工具结果与账本语义；容器内外文件映射和越界/网络/超时用例通过后，才将其列为支持。

## 与调试和 K4 的联动

- Bash 复用参数校验 → 审批 → 预算/取消检查 → 持久化派发 → 执行 → 保存结果。Sources 可以查看内置 Bash 审批，继续/单步不能绕过审批。
- Hybrid 只回放录制结果，不执行 Bash；Live 属于真实执行，需要当前实例的完整访问权限与 Bash 开关。
- K4 动态回退保留已完成结果；同一回退运行中相同参数的已完成 Bash/编辑等副作用复用记录。新的运行仍可能再次执行，不提供跨任务全局恰好一次保证。
- 已派发但结果未落盘时，原有恢复逻辑保留 outcome_unknown，不自动重新执行。取消、超时、强杀不能当作文件或远端事务回滚。
- 独立文件目录评测与自然语言诊断禁用 Bash。具备 Bash 的普通工作区案例可以走 K4，但测试命令的外部状态需由任务设计控制。

## 完成度和下一批

K1 的最小模型/工具/检查闭环已具备，本版补齐七类编程动作并提供本地实执行回归，可以开始实际编程任务测试。此前用“K1 基本完成”概括，未明确指出缺少 Bash，会让人误以为已有完整编程 Agent 能力；现在将两者分开记录。

K4 候选、独立验证、固定配对对照、产物规则、文件初始化与回退基础链路已经实现；本版验证 Bash 回退不重复副作用。真实业务质量、收益、生产负载及沙盒执行仍需对应验收。

后续顺序：Kun 执行环境与容器隔离；Bash 流式输出/大输出产物；read 分页及常用 glob；真实模型/Windows/浏览器验收。自动上下文压缩、更多 provider、自动 Skills 激活和 PiG 插件生态单独评估，不作为本版完成声明。

证据与复验入口见 [KUN-VALIDATION](KUN-VALIDATION.md)；功能完成度见 [KUN-PROGRESS](KUN-PROGRESS.md)。
