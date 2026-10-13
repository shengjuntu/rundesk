# 当前验证

RunDesk **0.48.0** / Kun **0.16.0** / worker 协议 **16**。本轮验证日期 2026-10-13。

已完成：

- `go test ./...` 全部通过。
- `go test -race ./internal/app -run 'TestRuntimeImage|TestBuild|TestSetup' -count=1` 通过。
- 新增初始化任务测试：注册应用前准备镜像；build/pull/local；同配置在途去重；相同配方缓存；不改通用助手；错误镜像、缺失镜像和工具失败不标就绪；终态取消不改变结果；管理员限制与 HTTP 幂等；Docker-only 初始化无需本机 Codex。
- 11 项 Python 配方测试通过；内嵌 ZIP 与 CLI 编译结果一致，文档链接/OpenAPI 检查通过。
- 实际初始化 JavaScript 在 Node DOM fixture 中通过：只读加载、后台提交、取消、重试、关闭、重新打开及离线就绪提示；原有初始化回归也通过。这不是浏览器视觉验收。
- npm 锁文件实际安装成功；mcporter 0.7.3 与 bird 0.8.0 能输出版本，真实 mcporter 接受生成的 Exa 配置。未调用外部搜索工具。bird 在 npm 已弃用，见配置评估。
- 两端真实 HTTP / 业务 MCP 联调通过：注册、权限配置、Exa 检索票据、证据和报告持久化、重复启动、停止及容器复用逻辑。Docker CLI、Codex 和搜索均为测试替身。

限制：当前环境没有 Docker Engine，**没有实际构建本轮镜像，没有容器内在线模型、认证或搜索质量验收**。这是一份经过代码/依赖与协议验证的源码交付，不是已发布、已部署的镜像。真实部署需按初始化向导完成镜像准备，再验证认证、Exa 与业务研究。

原有 Kun 内核和协议未改动。生产升级前应保留数据目录与旧镜像；本轮不会自动切换既有应用绑定。
