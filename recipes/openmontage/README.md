# OpenMontage 独立制作环境 1.1.1

这是原生 [OpenMontage](https://github.com/calesthio/OpenMontage) 的 Codex Docker 配置，固定源码提交 `c8066452325cb57c8fc1ba8c2a71b6e8842e6b65`。它保留上游 pipeline、导演 Skills、checkpoint、原生产物与 Remotion/HyperFrames 两种合成方式；不改造为自研 Video Builder，也不要求使用 Video App 的剪辑 DSL。

## 构建和接入

```bash
python3 scripts/recipes.py prepare openmontage --codex-version X.Y.Z --output ./openmontage-context
python3 scripts/recipes.py build openmontage --codex-version X.Y.Z --image rundesk-openmontage:1.1.1
```

X.Y.Z 必须是实际选定的精确 Codex 版本。`prepare` 在本地生成完整上下文；源码随配方放在 `vendor/source.tar.xz`，构建先校验 SHA-256，再由已有 Python 3 标准库解包，不下载 GitHub 仓库，也不新增 `xz` 系统工具。

原完整归档 75,957,235 字节，0.69.0 使用的 gzip 包 17,846,107 字节，本版 XZ 包为 **10,296,296 字节（约 10.3 MB）**，比上版再减少约 42.3%。在原三份演示视频之外，再省略四张文档预览图和一个未引用示例 Logo；其余 **2,163 个文件**的内容、路径和可执行权限语义保持一致。32 MiB 字典压缩可利用重复字节，不删除重复的 Skills、音效、纹理或字体路径，也不将它们变为链接。逐文件校验、压缩信息和完整省略清单见 [source-bundle.json](upstream/source-bundle.json)。保留的原文档仍可能引用这些已省略的预览图；可从记录的原归档获取预览。

Python requirements、Remotion composer、HyperFrames 0.8.87/GSAP 3.15.0 的 pip/npm 安装仍需使用配置的源。依赖安装先于 provider 覆盖文件复制；只修改覆盖文件或 Skills 时可沿用依赖层。公共 browser 基础层继续复用。没有安装 Piper、本地生成模型或 GPU 权重。

新内置配方版本为 1.1.1；之前导入的自定义配方或已冻结构建仍保留原内容，需要从新版配方发起新构建才能使用内置源包。

在 RunDesk 为此镜像创建专用实例/工作区（可登记同名 application runtime preset），配置 Codex 认证。初始化 Seed 含 Video App media MCP 服务入口；在该实例 MCP 配置的 env 填 VIDEO_APP_MCP_URL、VIDEO_APP_TOKEN、VIDEO_APP_PROJECT_ID，不将真实凭据写入镜像或示例。宿主 shell export 不会自动进入已创建容器。

在 Codex 使用 `$use-openmontage`。该 Skill 指向原始 AGENT_GUIDE、pipeline_defs 和 stage directors，保留上游创意及审批流程。生成工具以原生 registry provider 接入：video_app_tts、video_app_image、video_app_video；它们通过共同 MCP bridge 调用 Video App 已配置的 IndexTTS、图像服务和视频服务，返回真实本地素材及原始 asset/job 票据。未支持的上游生成参数明确报错，禁止悄悄改用云厂商或 Piper。

## 持久工作区与结果

RunDesk 工具容器根目录只读。使用 openmontage-python 从托管工作目录启动，首次在该目录准备可写原生源码/composer；原始镜像源码保留在 /opt/upstreams/openmontage。项目、媒体、checkpoint 和结果留在工作目录。原生 Remotion 会向 composer staging/public/cache 写数据，因此只对 /opt chmod 不能解决只读限制。

这里交付原生制作及生成服务接入，不提供自动将原生 final.mp4 登记为 Video App 成片版本的适配；可以使用原生产物目录或 RunDesk 产物浏览/下载。模型服务配置/任务追踪仍由 Video App 与 RunDesk 分工管理。不能把初始化检查、MCP 连通或生成素材成功等同于完整成片质量验收。

## 来源与许可

保留 OpenMontage 的代码、Skills、文档、运行素材和 AGPLv3 LICENSE，仅省略记录中的演示／预览媒体。OpenMontage 专用 provider 覆盖文件也以 AGPL-3.0-only 提供，源码位于本目录 `upstream/overlays` 并复制到镜像原生 tools/lib；不复制进自研 video-build。分发镜像时保留完整对应上游源码、这里的集成源码与许可；这些文件亦在镜像中可用。Remotion、HyperFrames 及其他依赖遵循各自许可。本目录不会改变自研 Video Builder 的源码或许可。

## 验证边界

本轮会检查固定 source 摘要、真实 BaseTool/registry/selector 接口、生成错误恢复与下载素材票据，并生成离线 Docker 配方。无 Docker Engine 的环境不能宣称镜像已构建、真实模型已生成或 OpenMontage 全流程已验收。容器内运行 python3 /opt/rundesk-profile/tools/profile_doctor.py --json；其检查不调用真实模型。


维护者可从原始归档复现源包；此命令只读取本地文件，先核对原始归档的固定 SHA-256，拒绝覆盖已有目标：

```bash
python3 scripts/package_openmontage_source.py --source /path/to/original-openmontage.tar.gz --output /new/vendor/source.tar.xz --manifest /new/upstream/source-bundle.json
```

原归档 URL、原始/当前摘要、每个保留文件的摘要与被省略文件的原因均记录在上述清单。主 RunDesk 二进制不嵌入这个归档；部署时应保留随包的 `recipes/`。
