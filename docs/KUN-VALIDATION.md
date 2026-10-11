# Kun 0.12 / RunDesk 0.38.1 验证记录

验证日期：2026-10-08。Go 1.25.12，Kun 0.12.0 / worker protocol 12 / fork schema 3。本批已补齐 0.38.0 未确认的最终回归结果，所有下列本地检查均已取得退出码 0；未新增业务功能或依赖。

| 项目 | 已确认结果 |
| --- | --- |
| 全量 Go | `go test ./... -count=1 -timeout=300s` 通过；app 81.621s，包含未知终态和零游标后端用例 |
| 静态检查 | `go vet ./...` 通过 |
| 专项 race | 评分、对照、工具结果假设的 app 测试通过，3.322s |
| Linux 构建与 CLI | 两个程序构建通过；版本、worker hello/EOF、旧协议拒绝通过 |
| Windows 交叉编译 | 两个 amd64 程序编译通过；未在 Windows 运行 |
| JS 与界面 | 75 个 JS/CJS 语法检查、9 组 Node 交互检查通过 |
| 实际 HTTP | 本次重新构建的 RunDesk + Kun + 本地模型夹具通过；来源/普通 Hybrid/假设 Hybrid 共 6 次模型调用；固定规则、评分报告和幂等读写不增加模型调用 |
| API 契约 | 157 paths / 189 operations；引用解析、参数唯一性、管理员标识及新增 POST 必填幂等头通过 |

## 原始证据

- [验证运行汇总](validation/kun-0.38.1/summary.json)：开始/结束时间、每项状态、退出码和源码 SHA256。
- [全量 Go](validation/kun-0.38.1/go-test.log)、[vet](validation/kun-0.38.1/go-vet.log)、[专项 race](validation/kun-0.38.1/go-race.log)。
- [当前平台构建](validation/kun-0.38.1/build.log)、[Windows 交叉编译](validation/kun-0.38.1/windows.log)。
- [JS 语法](validation/kun-0.38.1/syntax.log)、[界面交互](validation/kun-0.38.1/ui.log)、[实际 HTTP](validation/kun-0.38.1/http.log)。

评分相关测试覆盖固定规则/范围、完整回复超出显示节选的评分、缺失/被替代/超大回复、未知终态、固定零游标、报告跨日志删除/重启持久化、幂等与权限。已通过本次全量和专项 race；不再依赖上一版未取回的进程输出。

## 一键复验

在源码根目录执行；需要 Python 3、Node、`go.mod` 指定版本的 Go，以及 race 所需的 C 编译器：

```sh
python3 scripts/validate-kun.py --output validation/kun-local-1
```

Go 未在 PATH 中时增加 `--go /absolute/path/to/go`。可用 `--checks go-test,go-vet,go-race` 选择子集；HTTP 复验必须同时选择 `build`。每次换一个输出目录，脚本拒绝覆盖已有目录。默认执行所有 8 组检查。

脚本在每项开始/结束时原子更新 `summary.json`，并直接写入对应日志；某项失败不会被后续成功覆盖。超时/中断保存明确状态；若进程被外部强制终止，`running` 表示未完成，不能视为通过。汇总中的 `sourceSHA256` 覆盖 `cmd`、`internal`、`scripts`、`go.mod`、`go.sum` 与 Makefile；说明文档和生成的验证结果不计入此指纹。

## 尚未验收

真实浏览器布局/焦点/移动端、真实 Codex/远程模型/MCP、Windows 运行、生产负载与独立验证集仍未验收。Node DOM 检查和本地模型夹具不能替代这些项目。本批没有重复尝试受环境限制的浏览器启动。

机器摘要见 `kun-build-validation.json`、`kun-contract-validation.json`、`kun-cli-validation.json` 和 `kun-evaluation-http-validation.json`。0.38.0 的部分验证记录原样保留在 `*-0.38.0.*` 中；本次记录独立保存。
