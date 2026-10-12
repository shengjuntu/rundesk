# Kun 0.16 / RunDesk 0.44.0 验证记录

验证日期：2026-10-11（北京时间，日志为 UTC）。Go 1.25.12；Kun 0.16.0 / worker v16 / fork schema 3。八组本地发布回归全部退出 0；交付源码与回归指纹一致。没有调用付费远程模型。

| 检查 | 结果 |
| --- | --- |
| 全量 Go | 所有包通过，77.56 秒；包含新增 Bash、编辑/搜索、Host 权限和原 K4 测试 |
| 静态与竞态 | vet 全包通过；专项 race 通过，40.972 秒，包含 Bash/搜索/编辑及快速回退 |
| 构建与协议 | Linux 两程序；Windows 两程序交叉编译；CLI/hello/EOF/旧协议 15 拒绝，bash/codingTools 能力位通过 |
| 界面 | 85 个 JS/CJS 语法、12 组 Node DOM 交互通过；Bash 开关、审批和超时配置往返通过 |
| 新编程 HTTP | 7 次本地模型调用，真实宿主/worker、七工具链与生成 Python 实执行通过 |
| 既有 HTTP | 评测 6 次、K4 48 次、固定文件目录 24 次本地模型调用；原有通过/拒绝/停止/重启路径通过 |
| 契约 | 180 paths / 216 operations；本地引用、参数唯一、配置管理员权限、控制的 run/approvals scope、幂等头与可选 Bash 字段通过 |
| 兼容与依赖 | 引擎/worker 升级拒绝旧执行；既有序列化/指纹回归通过；go.mod/go.sum 未修改 |

## 新增能力的实执行证据

| 场景 | 观察 |
| --- | --- |
| 生成代码运行 | write_file 生成 Python → edit_file 改求和输入 → find/grep/list → Bash 运行 → read_file 返回 `6\n`；七工具均成功 |
| 权限与审批 | workspace-write/read-only 不能启用宿主 Bash；权限收紧后启动拒绝；默认询问且文件在批准前不存在；拒绝/never 不执行；resume 不能代替 approve |
| Shell 行为 | 管道、重定向、带空格路径、UTF-8、stdout/stderr 分离、非零退出码 7；cwd/export 不跨调用保留，BASH_ENV 不注入 |
| 有界输出 | stdout/stderr 各产生 100003 字节，持续排空并仅保留各 65536 字节尾部，截断标记和末尾文本正确 |
| 取消与超时 | 直接工具及实际 HTTP 路径均验证；Linux 普通子进程的延迟文件写入被清理阻止；不保证主动脱组/强杀 worker 情况 |
| 编辑与文件边界 | 对原文多处匹配；模糊/缺失/重叠失败不改原文件；越界/符号链接、只读写入拒绝；FIFO 不阻塞读/写/编辑 |
| 搜索 | UTF-8、字面/大小写/简单 glob/行号，匹配限额和截断；跳过二进制/.git；非法路径、正则和取消拒绝 |
| Hybrid 与 K4 | Bash Hybrid 录制回放不创建文件；快速路径追加后强制断言回退，模型再次发出相同命令时复用账本，文件仅追加一次 |
| 固定目录与重启 | 启用 Bash 时不能启动独立文件目录评测；完成的工具/审批证据重启后不变，不重执行 |

以上是确定性本地模型驱动的真实进程测试，证明执行链路及约束，不代表远程模型会稳定规划这些步骤或业务收益。Windows 仅交叉编译；Node DOM 不代替浏览器布局、焦点与移动端验收。OS 沙盒、Kun Docker 托管、长文件分页、交互/持久 Shell 与实时输出流尚未实现。

## 复验与原始记录

```bash
python3 scripts/validate-kun.py --go /path/to/go --output validation/kun-0.44.0-recheck
```

需要 Go 1.25.12、Node、Python3、Bash；race 需要 C 编译器。`kun-coding-http-test.cjs` 使用本机 Python3，所需解释器由执行环境提供。

- [最终八组汇总](validation/kun-0.44.0-final/summary.json)，同目录保存每组原始日志。
- [编程 HTTP](kun-coding-http-validation.json)、[评测 HTTP](kun-evaluation-http-validation.json)、[K4 HTTP](kun-k4-http-validation.json)、[独立目录 HTTP](kun-fixtures-http-validation.json)。
- [CLI](kun-cli-validation.json)、[契约](kun-contract-validation.json)、[构建](kun-build-validation.json)、[配置界面](kun-harness-ui-validation.json)。
- 补充核对脚本 [verify-0.44.0.py](validation/verify-0.44.0.py)，在本版最终回归结束后核对协议/契约及源码指纹。
- 0.43 顶层证据保留于 `validation/kun-0.43.0-evidence/`，旧验证说明为 [KUN-VALIDATION-0.43.0](KUN-VALIDATION-0.43.0.md)，此前各版日志继续保留。

本版源码指纹：`0f9fa998222ea6b13a85e326de3313839c29b6d6742ed9a141a841ec8296da87`。该指纹覆盖 go.mod/go.sum/Makefile 和 cmd/internal/scripts，不包括验证日志自身。
