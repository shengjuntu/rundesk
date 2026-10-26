# video-build 1.4.0

RunDesk 驱动 Codex 自主理解文稿、分镜、选材、生成和检查；Video App MCP 提供素材、图片/视频生成、TTS，以及自由 HyperFrames 工程的提交、检查和渲染。这个执行环境提供 **Node.js 22+、HyperFrames 0.8.87、GSAP 3.15.0、Chromium、FFmpeg、Pillow 和中文字体**。不打包 OpenMontage 的代码、Schema 或 Skills。

## 构建与升级

```bash
python3 examples/video-build/run.py prepare --codex-version YOUR_EXACT_CODEX_VERSION --output ./video-build-context
```

检查生成上下文后构建，或使用 RunDesk 初始化页面的 video-build 配置。新的默认镜像使用 profile `1.4.0`、`io.rundesk.hyperframes.version=0.8.87`、`io.rundesk.gsap.version=3.15.0` 标签；已有镜像必须重新构建，并在应用环境中明确选择和更新/重建。升级保留原工作目录、HOME 与 Codex 认证，不会自动替换正在运行的环境。

HyperFrames 和 GSAP 的直接与传递 npm 依赖由 `_shared/hyperframes/package-lock.json` 锁定，并以 `npm ci` 安装；元数据记录锁文件 SHA-256。基础镜像、Debian apt 和系统 Chromium 仍未完全锁定，跨平台构建须单独验收。Docker 不在首次任务中下载浏览器：`HYPERFRAMES_BROWSER_PATH=/usr/bin/chromium`；默认关闭 HyperFrames 遥测。镜像不包含生成模型、服务密钥或 Docker socket。

## 自由工程与工具

- `hyperframes` 在 PATH 中，安装根目录为 `/opt/hyperframes`。
- GSAP 可从 `/opt/hyperframes/node_modules/gsap/dist/gsap.min.js` 复制到自己的工程，保留版权及许可证头。
- Codex 自由编写 HTML、CSS、JavaScript 和动画时间线；Skill 提供工具用法与质量检查，不把镜头、特效或内容限制成固定模板。
- 素材使用明确来源和 ID，配音时长用实际媒体探测与对齐结果；自由动画工程和主剪辑 DSL 的职责分别明确，避免两份时间线无关联覆盖。
- 字体要有明确的本地 `@font-face`，即使用系统字体也需声明；发布工程时保留字体许可。CSS 中只写字体名可能被 HyperFrames 检查拒绝。

典型任务是准备素材与文字关联、生成配音、安排时间线、制作动画、检查小样、局部精修。Codex 可以自行调整步骤并回到上一阶段；不得把工具成功当成人工内容验收。

## App 交互与工作目录

新 HyperFrames 主流程通过 Video App MCP 提交工程文件和素材引用、调用检查和渲染。工程保存在 App 自己管理的目录中，执行容器不必与 App 共享工作目录；TTS、图片和视频生成也通过 MCP 调用。浏览器执行动画属于制作代码执行，应使用专用执行环境。

RunDesk 仍会将应用工作目录挂入自己的执行容器，供 Codex 保存草稿、脚本和检查结果。需要继续使用旧 video-use 交换协议时，管理员才将这个专用工作目录同时挂给 App 的 `VIDEO_EXCHANGE_ROOT`，并执行交换探测。不得为了新主流程随意挂载 App 私有数据目录或 Docker socket。

## 可选 video-use 兼容

默认不安装 video-use，也不安装它的大组音频分析 Python 依赖。确实需要运行旧脚本素材适配器时显式启用：

```bash
python3 examples/video-build/run.py prepare --with-video-use --codex-version YOUR_EXACT_CODEX_VERSION --output ./video-build-legacy-context
```

此时保留源码 commit `43cfc566833548093455c9d4a46bd69913b7e552`、来源归档 SHA-256、精确 Python 传递依赖锁、`/opt/video-use` 和 adapter `2` 镜像标签。新旧配置均提供 `video_use_adapter.py --version` 的静态协议2报告；它只描述兼容适配器，不证明上游编辑器已安装。旧 EDL、validation、completion 归档继续按原协议读取，细节见 [VIDEO-USE-COMPAT.md](VIDEO-USE-COMPAT.md)。

## 离线验收

```bash
python3 /opt/rundesk-profile/tools/profile_doctor.py --json
python3 /opt/rundesk-profile/tools/hyperframes_smoke.py --out ./hyperframes-smoke-001
```

doctor 核对实际 Node 最低版本、npm 包版本、浏览器、字体和工具，并真实渲染本地 HTML+GSAP 动画。烟测先执行 HyperFrames check，再生成1秒H.264，检查尺寸、时长、解码后帧内容是否变化；另用 FFmpeg 生成2秒视频+音频检查H.264/AAC。可选 video-use 才执行其来源、依赖和旧适配器合成检查。doctor 不安装软件、不下载浏览器、不调用模型或生成服务。

`dependency-validation.json` 和 `validation/` 保存本轮实际宿主离线渲染及契约测试记录。宿主验收使用 Node 24 和现有 Chrome Headless Shell，不等于 Node 22 Debian Chromium Docker 镜像验收；本轮无 Docker CLI，未构建镜像，也未验收真实 IndexTTS、模型生成或 Codex 自主制作闭环。
