# 0.11.0 验证记录

本次验证在 Linux amd64、Go 1.27.1 环境执行。

- `go test -race ./...`：全部通过。包含跨项目访问、默认拒绝、只读写入、个人登录、退出、重置、停用、授权修订、登录过期、事件流撤销、幂等提交隔离和本机执行降级拦截。
- `go vet ./...`：通过，无输出。
- Linux amd64 静态构建、Windows amd64 交叉编译通过。Windows 未运行验证。
- `scripts/users-smoke.cjs`：12 项通过，覆盖管理员创建用户、桌面/移动界面、个人登录、任务执行、过程展示、文件下载、越权请求、刷新恢复、审批、只读成员、停用与重置。
- `scripts/images-smoke.cjs`：12 项通过，验证镜像登记、固定、版本漂移、显式更新和失败前置检查。

- `scripts/docker-smoke.cjs`：12 项通过，验证项目环境、进程监管、宿主机数据保留和错误展示。
- `scripts/credentials-smoke.cjs`：7 项通过，验证原应用凭据创建、任务提交、一次性展示和撤销。

浏览器报告和截图保存在 docs 下对应 0.11.0 文件中。Docker 相关测试使用 fake Docker CLI，执行协议使用内置 demo App Server，没有调用真实模型。不得把本次结果视为真实 Docker、Codex 认证、网络、MCP 服务或远程部署的实机验收。

旧版本报告仍随源码保留，不代表旧版所有浏览器脚本在本次重跑。历史脚本恢复范围见 SOURCE-RECOVERY.md。
