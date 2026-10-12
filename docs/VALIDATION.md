# 当前版本验证

RunDesk 0.45.0 / Kun 0.16.0 / worker 协议 16。2026-10-11 完成本地发布验证，八组检查均有通过记录。

| 检查 | 结果与范围 |
| --- | --- |
| 全量 Go | 通过，包含新增启动、凭据缺失、原子保存和管理接口授权测试 |
| go vet | 通过 |
| 专项 race | 通过，覆盖启动保存/安装、Kun 评测、K4、Bash 和存储 |
| Linux 构建 | RunDesk 与 Kun 均通过 |
| Windows 交叉编译 | 两个 amd64 程序均通过；没有原生运行验收 |
| JavaScript 语法 | WebUI 与脚本全部通过 |
| DOM 交互 | 13 套通过，包括首次探测、Kun 独立配置、可选安装、安装中保存阻止与显式启用 |
| 实际宿主/worker HTTP | 编程工具、假设评测、K4 与独立文件目录四套通过；本地模型调用分别为 7、6、48、24 次 |

文档检查通过：48 份入口/技术文档的本地链接、183 个 OpenAPI 路径、版本、启动管理权限及安装幂等约束。Python 客户端示例通过语法检查；应用 API 另由 Go 契约测试覆盖。

汇总见 [summary.json](validation/current/summary.json)。首次 [完整运行](validation/current/regression/summary.json) 中 Go、vet、race 等通过，HTTP 因测试仍固定断言旧版本 0.44.0 而中止；修正为读取当前源码版本，同时补充向导隐藏字段与长路径样式。此后没有修改 Go 代码，针对受影响部分重新构建两平台并复验语法、DOM 与全部 HTTP 流程，见 [最终复验](validation/current/final/summary.json)。两个运行的原始状态与日志均保留，不把首次失败改写成成功。

最终源码指纹：`8ba6e3d65eecf267ab5d810782468e8ce599f4b9f89d6098073a0145cb1b41ff`。它覆盖 Makefile、go.mod、go.sum、cmd、internal、scripts；分发文件逐项摘要见根目录 SHA256SUMS。

复验入口：

```bash
python3 scripts/generate-openapi.py
python3 scripts/check-docs.py
python3 scripts/validate-kun.py --go /path/to/go --output validation/new-run
```

要求 Go 1.25.12、Node、Python 3；race 需要 C 编译器。回归使用本地模型/协议 fixture，不消费远程模型。独立的安装测试使用 npm fixture，验证固定版本、托管路径、回执复用和不自动启用，不从 registry 下载。浏览器布局、真实 npm 下载/认证、原生 Windows、Docker、远程模型和生产业务质量仍需部署验收。
