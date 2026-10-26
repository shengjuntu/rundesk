# OpenMontage 独立制作环境 1.0.0

这是原生 [OpenMontage](https://github.com/calesthio/OpenMontage) 的 Codex Docker 配置，固定源码提交 `c8066452325cb57c8fc1ba8c2a71b6e8842e6b65`。它保留上游 pipeline、导演 Skills、checkpoint、原生产物与 Remotion/HyperFrames 两种合成方式；不改造为自研 Video Builder，也不要求使用 Video App 的剪辑 DSL。

## 构建和接入

```bash
python3 examples/openmontage/run.py prepare --codex-version X.Y.Z --output ./openmontage-context
python3 examples/openmontage/run.py build --codex-version X.Y.Z --image rundesk-openmontage:1.0.0
```

X.Y.Z 必须是实际选定的精确 Codex 版本。prepare 在本地生成上下文；Docker build 才下载固定 codeload 源归档，验证 SHA-256，安装上游 Python requirements、锁定 Remotion composer，以及 HyperFrames 0.8.87/GSAP 3.15.0。源归档约 76 MB，不塞进 RunDesk 二进制。没有安装 Piper、本地生成模型或 GPU 权重。

在 RunDesk 为此镜像创建专用实例/工作区（可登记同名 application runtime preset），配置 Codex 认证。初始化 Seed 含 Video App media MCP 服务入口；在该实例 MCP 配置的 env 填 VIDEO_APP_MCP_URL、VIDEO_APP_TOKEN、VIDEO_APP_PROJECT_ID，不将真实凭据写入镜像或示例。宿主 shell export 不会自动进入已创建容器。

在 Codex 使用 `$use-openmontage`。该 Skill 指向原始 AGENT_GUIDE、pipeline_defs 和 stage directors，保留上游创意及审批流程。生成工具以原生 registry provider 接入：video_app_tts、video_app_image、video_app_video；它们通过共同 MCP bridge 调用 Video App 已配置的 IndexTTS、图像服务和视频服务，返回真实本地素材及原始 asset/job 票据。未支持的上游生成参数明确报错，禁止悄悄改用云厂商或 Piper。

## 持久工作区与结果

RunDesk 工具容器根目录只读。使用 openmontage-python 从托管工作目录启动，首次在该目录准备可写原生源码/composer；原始镜像源码保留在 /opt/upstreams/openmontage。项目、媒体、checkpoint 和结果留在工作目录。原生 Remotion 会向 composer staging/public/cache 写数据，因此只对 /opt chmod 不能解决只读限制。

这里交付原生制作及生成服务接入，不提供自动将原生 final.mp4 登记为 Video App 成片版本的适配；可以使用原生产物目录或 RunDesk 产物浏览/下载。模型服务配置/任务追踪仍由 Video App 与 RunDesk 分工管理。不能把初始化检查、MCP 连通或生成素材成功等同于完整成片质量验收。

## 来源与许可

保留 OpenMontage 完整源码和 AGPLv3 LICENSE。OpenMontage 专用 provider 覆盖文件也以 AGPL-3.0-only 提供，源码位于本目录 overlays 并复制到镜像原生 tools/lib；不复制进自研 video-build。分发镜像时保留完整对应上游源码、这里的集成源码与许可；这些文件亦在镜像中可用。Remotion、HyperFrames 及其他依赖遵循各自许可。本目录不会改变自研 Video Builder 的源码或许可。

## 验证边界

本轮会检查固定 source 摘要、真实 BaseTool/registry/selector 接口、生成错误恢复与下载素材票据，并生成离线 Docker 配方。无 Docker Engine 的环境不能宣称镜像已构建、真实模型已生成或 OpenMontage 全流程已验收。容器内运行 python3 /opt/rundesk-profile/tools/profile_doctor.py --json；其检查不调用真实模型。
