# RunDesk 0.10.1 验证记录

日期：2026-10-04。环境：Linux amd64、Go 1.27.1、Chromium headless。Docker CLI 使用明确标识的测试替身，模型使用原生 Demo JSONL 协议模拟器。

| 检查 | 结果 |
| --- | --- |
| Go 全包竞态测试 `go test -race ./...` | 通过，6 个含测试包 |
| 镜像管理与 OpenAPI 契约补充测试（竞态） | 通过 |
| `go vet ./...` | 通过 |
| Linux amd64 静态构建 | 通过 |
| Windows amd64 交叉构建 | 通过；未在 Windows 实机运行 |
| 镜像页面浏览器检查 | 12 项通过 |
| Docker 页面与 App Server 连接回归 | 12 项通过 |
| 通用助手、应用、Skills/MCP 界面回归 | 13 项通过 |
| JavaScript/Python 语法、OpenAPI 字段与管理员权限、diff 格式 | 通过 |

本次共 37 项浏览器检查。记录见 `images-validation-0.10.1.json`、`docker-validation-0.10.1.json`、`product-validation-0.10.1.json`，截图见 `screenshots/0.10.1/`。

重点验证：
- 同标签指向新镜像会产生新版本，原记录与环境固定 ID 不变。
- 登记不修改应用；选择固定版本不关闭旧环境连接。
- 同镜像的登记不会让已有环境无谓升级；有差异的环境明确显示待更新。
- 显式更新按选定 Image ID 执行，即使原标签再次变化也不采用意外镜像。
- 更新前镜像缺失时，不停止或删除旧运行容器。
- 活动连接、过期环境 revision、过期应用 revision 阻止更新。
- 宿主机数据保留，选择旧镜像后可重新部署。
- Docker 离线时目录可读，检查失败原因持久化；恢复后可重新核对。
- 元数据缺失显示未提供，不暴露镜像 Config.Env 和无关标签。
- 应用凭据不能访问镜像管理 API，版本记录不能跨应用选用。
- 桌面与 390px 移动端布局无横向溢出，环境更新操作放在长元数据详情之前。

限制：没有真实 Docker daemon，未执行真实镜像构建、拉取、容器工具或真实模型/MCP 组合。测试替身不能证明主机 Docker、网络、权限与镜像实际兼容。已有 `scripts/docker-real-smoke.py` 供部署主机验证运行层；镜像元数据和构建参数仍需使用实际构建镜像验收。内置镜像构建与完整多用户不在本版范围。
