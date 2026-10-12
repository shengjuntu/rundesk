# 当前版本验证

RunDesk **0.47.0** / Kun **0.16.0** / worker 协议 **16**。2026-10-12 完成应用运行环境 API 与 news2douyin 0.13.0 联调。当前环境没有 Docker Engine 或 Codex，容器和模型路径使用测试替身；交付源码，不交付已验收镜像。

| 检查 | 结果与范围 |
| --- | --- |
| Go 兼容性 | application、Docker、environment、v1、seed 和 containerruntime 相关测试通过 |
| 应用环境并发 | 新增运行环境测试通过 race；并发 prepare 只创建/启动一次，活动连接保持，拒绝绑定/归属越权和待更新配置 |
| HTTP 与业务 MCP | 真实两端 HTTP 服务，覆盖注册、镜像配置、MCP 初始化/工具发现、启动研究、检索记录、证据与报告、轨迹和停止 |
| 重复启动 | 新闻重复点击复用运行；完整联调只创建一个对应环境；底层为 Docker CLI fixture |
| news2douyin | 274 项测试通过，19 项可选媒体测试因缺少 Pillow/字体等依赖跳过；DOM 替身交互契约 5 项通过 |
| Linux 构建与契约 | RunDesk/Kun 构建、版本输出和文档契约检查；OpenAPI 185 条路径、221 个操作 |

原始记录见 [汇总](validation/current/summary.json)、[兼容性](validation/current/go-compatibility.log)、[race](validation/current/go-runtime-race.log) 和 [两端 HTTP](validation/current/http-summary.json)。新闻源码包另含完整 Python、UI 和 HTTP 联调入口。

本轮没有修改 Kun 内核，不将旧版全量 Kun、媒体或 Windows 日志作为本版验证。上一版配置目录与媒体实跑记录保留在 0.46.0 历史交付中。无浏览器视觉验收：Chromium 下载失败，DOM 契约检查不证明真实布局。

## 部署仍需验证

Docker 实际构建与权限、容器到新闻服务回调、该环境的 Codex 认证、Exa/Wigolo 发现和真实搜索，以及带来源引用的完整模型研究。prepare 的 accountReady 只表示认证元数据检查通过；modelTested=false，不表示真实模型和新闻质量通过。Wigolo 的浏览器正文提取没有接入新闻证据导入。

## 复验入口

```bash
go test ./internal/app ./internal/containerruntime -run 'Test(Application|Docker|Environment|V1|Seed)' -count=1 -timeout=180s
go test -race ./internal/app -run TestApplicationRuntime -count=1 -timeout=180s
go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk
go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun
python3 scripts/check-docs.py
```

在安装研究开发依赖的 news2douyin 0.13.0 源码目录运行 `python tools/integration_docker_research.py --rundesk-root /absolute/path/to/rundesk --output /absolute/path/to/new-validation`。该脚本实际启动两端 HTTP，但显式使用 Docker/Codex/search fixture，不是部署验收。

源码指纹记录于 summary.json；按相对路径排序，将路径、NUL、文件字节、NUL 依次送入 SHA-256。覆盖 Makefile、go.mod、go.sum、cmd、internal、scripts、examples，排除 Python 缓存。分发文件摘要见根目录 SHA256SUMS。
