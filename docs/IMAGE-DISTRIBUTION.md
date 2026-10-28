# 公开预构建镜像 / Public prebuilt images

RunDesk 0.57.0 可下载已发布的预设镜像，也可将本机验证通过的镜像上传到仓库。维护者构建和测试后发布固定版本，使用者下载同一结果，避免在每台机器上重复执行 apt、pip 或 npm 安装。

**随包公开镜像目录为空。** 本版没有虚构的官方镜像空间，也没有实际上传到某个 GitHub 或 Docker Hub 账号。维护者提供真实目标仓库、发布权限并完成上传后，才能填入可供其他用户下载的地址。代码测试范围见[验证说明](VALIDATION.md)。

## 使用者：下载已有镜像

初始化仍是三步，第三步为“准备 Docker 环境”：

- **下载预构建镜像**：默认选项。选择适用的预设、版本和来源；没有发布条目时可填写真实镜像地址。目录中的地址固定到 Digest，手工地址也可使用有效标签。
- **本机构建**：需要修改 Dockerfile 或依赖时选择，继续提供构建源配置、后台日志与 Kun 失败修复。
- **已有本地镜像**：检查已在运行 RunDesk 的服务器 Docker Engine 中的镜像。

这些方式都需要受支持的 Linux 本机 Docker Engine。镜像准备任务关闭页面后继续执行；拉取后核对平台、预设身份、Codex 版本及离线工具，再使用固定 Image ID。下载成功不代表外部模型认证或业务任务已验收。镜像成功准备后可在构建中心查看，并显式创建实例；现有实例和应用绑定不会自动切换。

拉取失败时先检查地址、网络或仓库权限，再选择另一真实来源或重试原请求。系统不会静默改成本机构建。国内外来源可在目录中分别命名，但目录存在不证明该地区一定可达。

## 维护者：发布已验证的预设

1. 在选定的公开镜像托管服务中准备账号、仓库和写权限。GHCR 的公开镜像支持匿名拉取；Docker Hub 也支持公开仓库，但拉取受其使用限制。RunDesk 使用服务账户现有的 Docker 登录配置，不在网页收集仓库密码。
2. 使用 RunDesk 构建预设，在“镜像”中完成 **Offline 检查**和 **Smoke 测试**。两项成功记录须绑定同一个来源构建、固定 Image ID、冻结上下文和平台。界面显示发布条件及缺失原因。
3. 提交发布任务，填写完整的 `registry/namespace/repository:固定标签` 并确认上传。目标不能使用 `latest`。后台按固定本机 Image ID 打标签并推送；可查看状态、增量日志或取消。
4. 成功记录保留仓库 Digest 和完整固定引用。先在托管平台设置公开可见性，再使用发布记录中的“检查匿名访问”。该检查用独立空 Docker 配置读取固定引用的 manifest，并核对 Digest 和平台，不借用服务账户的登录凭据；结果分别保留 `publicAccessVerified`、检查时间和错误。它不下载完整镜像层，也不运行容器，不能替代目标网络上的完整拉取验收。上传成功本身不会将公开访问标记为已通过。
5. 将真实引用、预设版本、Codex 版本和平台加入公开镜像目录，再随源码/发行包分发目录文件。目录编辑不会自动更新其他已经安装 RunDesk 的服务器，也不会替换它们的运行实例。

当前发布入口面向带有完整冻结身份的六套受管预设。任意自定义镜像或缺少冻结预设身份的历史镜像不能通过该入口发布为 RunDesk 预设；需要重新建立可核对的来源和测试记录。一次构建自带的离线检查不替代发布所需的两项精确绑定测试。

发布记录持久保存。相同 `requestId` 的重试返回同一操作；参数改变应使用新请求。服务重启会将未确认的发布标记为中断，不自动再次推送。取消或中断不能撤回已经上传的层或标签，结果不明时先核对原任务和仓库。上传不会自动修改仓库可见性、公开镜像目录或默认源，Kun 修复也不会自行触发公共发布。

官方操作参考：[GHCR](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[GHCR 可见性](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility)、[Docker Hub 使用限制](https://docs.docker.com/docker-hub/usage/)、[固定 Digest 拉取](https://docs.docker.com/reference/cli/docker/image/pull/)。

## 公开目录文件

随包文件是 `config/public-images.json`，初始内容为：

```json
{
  "schemaVersion": 1,
  "items": []
}
```

默认首次从可执行文件旁或源码目录的 `config/` 复制到 `<data>/config/public-images.json`，此后使用管理员维护的文件。设置 `RUNDESK_PUBLIC_IMAGES` 时，该文件成为明确来源；页面保存也写入这个文件。显式路径错误应修正路径，不会悄悄换成默认目录。

“公开镜像目录”页面和 `GET /api/v1/setup/public-images` 返回内容摘要 `revision`；保存通过 PUT 携带相同 revision，避免覆盖他人同时修改的版本。已提交任务保留选定的引用与冻结输入；修改目录不改变历史任务。

每个 `items` 条目包含：

| 字段 | 含义 |
| --- | --- |
| `id` | 该发布条目的稳定标识 |
| `profileId` | 六套受支持的预设之一 |
| `profileVersion` | 预设的精确版本 |
| `codexVersion` | 镜像内 Codex 的精确版本 |
| `platform` | 如 `linux/amd64`，须匹配拉取服务器 |
| `sources` | 一个或多个来源，每项包含 `id`、`name`、`reference` |
| `sources[].reference` | 完整仓库引用，以实际 `@sha256:<64位摘要>` 固定内容 |

目录可保留历史版本；提交下载时还须与当前配方的冻结身份和宿主平台匹配。同一条目的多个来源须指向相同 Digest。不要把本机 Image ID 当作仓库 Digest；以发布成功记录或实际仓库查询的引用为依据。

公开镜像目录与 `build-sources.json` 分工不同：前者指定已经构建的镜像，后者指定本机构建期间下载依赖的通道。改动 pip、APT 或 npm 地址不会改变已经发布的镜像。

## 离线转移

本版没有新增 Docker tar 包的 WebUI 上传或下载接口。管理员可使用 Docker 的 `save` 保存镜像，转移文件后用 `load` 导入目标 Docker Engine，再在向导选择“已有本地镜像”。镜像文件不包含 RunDesk 数据目录、实例 HOME、认证或工作区数据，它们仍按原有部署方式管理。具体命令见 [Docker save](https://docs.docker.com/reference/cli/docker/image/save/) 和 [Docker load](https://docs.docker.com/reference/cli/docker/image/load/)。

## English summary

The third setup step defaults to downloading a published image, while retaining local builds and existing-image checks. Publishers must have successful offline and smoke records tied to the same managed preset build, image and frozen context before submitting a registry push. The service uses its Docker credentials for publication, records the resulting registry digest and does not change visibility or the public catalog automatically. A separate public-access check uses an empty Docker credential configuration to inspect the pinned manifest and verify its digest and platform; it does not download all layers or run the image.

The bundled `config/public-images.json` contains no invented release addresses. An actual registry account, a successful upload and anonymous-access verification are still required before distribution. Instance credentials, workspaces and application bindings remain separate. Docker archive transfer remains a manual `save`/`load` operation.
