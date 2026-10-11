# Kun 0.12 / RunDesk 0.39.0 验证记录

验证日期：2026-10-08。Go 1.25.12；Kun 0.12.0 / worker v12 / fork schema 3 保持不变。本次全部本地检查已取得退出码 0，原始日志与源码指纹一并保留。

| 项目 | 已确认结果 |
| --- | --- |
| 全量 Go | `go test ./... -count=1 -timeout=300s` 通过；app 77.596s |
| 静态检查 | `go vet ./...` 通过 |
| 专项 race | 任务集、评分、对照与工具结果假设测试通过 |
| Linux 构建与 CLI | 两个程序构建通过；版本、worker hello/EOF、旧协议拒绝通过 |
| Windows 交叉编译 | 两个 amd64 程序编译通过；未在 Windows 运行 |
| JS 与界面 | 77 个 JS/CJS 语法检查、10 组 Node 交互检查通过 |
| 实际 HTTP | 本次重新构建的宿主/worker 与本地模型夹具；任务集创建/幂等重试/完整读取/元数据列表不新增模型调用；原 Hybrid/假设/评分流程仍为 6 次模型调用 |
| API 契约 | 159 paths / 192 operations；引用、参数唯一性、新任务集 schema、管理员标识和必需幂等头检查通过；依赖未变 |

新增测试覆盖：案例标识唯一性、输入/规则/整集字节限制、20 案例边界、保留原始空白与文字、调用方切片隔离、幂等 Key 冲突、元数据分页不泄露正文、完整定义跨重启持久化、指纹篡改拒绝、严格字段/查询校验，以及应用/成员/别名前缀权限。界面检查覆盖显式保存、重复请求、复制为新草稿、只按文本渲染、分页与完整读取、身份不符和关闭后的响应丢弃；所有任务集操作仅使用定义接口。

## 原始证据

- [验证汇总与源码指纹](validation/kun-0.39.0/summary.json)。
- [全量 Go](validation/kun-0.39.0/go-test.log)、[vet](validation/kun-0.39.0/go-vet.log)、[专项 race](validation/kun-0.39.0/go-race.log)。
- [当前平台构建](validation/kun-0.39.0/build.log)、[Windows 交叉编译](validation/kun-0.39.0/windows.log)。
- [JS 语法](validation/kun-0.39.0/syntax.log)、[界面交互](validation/kun-0.39.0/ui.log)、[实际 HTTP](validation/kun-0.39.0/http.log)。

机器摘要见 `kun-build-validation.json`、`kun-contract-validation.json`、`kun-cli-validation.json`、`kun-task-sets-ui-validation.json` 和 `kun-evaluation-http-validation.json`。历史版本记录保持独立，不作为本次通过证据。

## 一键复验

需要 Python 3、Node、`go.mod` 指定版本的 Go，以及 race 所需的 C 编译器；在源码根目录执行：

```sh
python3 scripts/validate-kun.py --output validation/kun-local-1
```

Go 未在 PATH 中时增加 `--go /absolute/path/to/go`。可用 `--checks go-test,go-vet,go-race` 选择子集；HTTP 检查要求同次构建成功。每次使用新的输出目录，脚本拒绝覆盖历史结果。默认运行 8 组检查，UI 组包含 10 个脚本。

汇总在每项开始/结束时原子保存状态、退出码和时间，日志直接写入文件。超时或中断不会记为通过；外部强制结束后残留 `running` 表示未完成。源码指纹覆盖 cmd/internal/scripts、go.mod、go.sum 与 Makefile，文档和生成报告不计入。

## 尚未验收或实现

真实浏览器布局/焦点/移动端、真实 Codex/远程模型/MCP、Windows 运行与生产负载尚未验收。Node DOM 检查与本地模型夹具不能替代这些项目。

任务集目前是固定定义。批量执行、逐案例运行/结果绑定、汇总评分、多次采样和独立验证集隔离尚未实现；当前 API/界面测试通过不代表这些能力或 K4 已完成。
