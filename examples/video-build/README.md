# video-build 1.3.0

文字驱动制作分为 **素材准备和对齐 → 剪辑 → 精修**。Video App MCP 提供 TTS、图片/视频生成、素材与项目服务；RunDesk 驱动 Codex；这个工具镜像提供固定版本 video-use、FFmpeg、中文字体和脚本素材适配器。镜像不内置生成模型或服务密钥。

video-use 固定到 `43cfc566833548093455c9d4a46bd69913b7e552`，源码和 MIT 许可证已随构建上下文打包，构建时不再追踪 GitHub main。Python 直接和传递依赖由 `video-use-requirements.txt` 固定版本，构建使用 `--no-deps` 并执行 `pip check`；基础镜像和 apt 尚未完全锁定。

## 构建与升级

```bash
python3 examples/video-build/run.py prepare --codex-version YOUR_EXACT_CODEX_VERSION --output ./video-build-context
```

也可使用 RunDesk 初始化页面构建内置 video-build 配置。新镜像要求 profile `1.3.0`、video-use commit 和 adapter `2` 标签；已有旧镜像不会被误判为新功能。构建后须在应用环境中明确选择新镜像并更新/重建执行环境，保留原工作目录、HOME 与 Codex 认证。镜像内源码 `/opt/video-use` 只读，任务写入现有工作目录。

## 共享工作目录

RunDesk 已将应用工作目录以相同绝对路径挂入任务容器，无需新开 Docker socket 或任意目录挂载权限。管理员把**这个专用应用工作目录**同时挂载给 Video App，例如：

```yaml
services:
  video-app:
    environment:
      VIDEO_EXCHANGE_ROOT: /exchange
    volumes:
      - /srv/rundesk/workspaces/video-app:/exchange
```

这里的宿主路径必须替换成实际 RunDesk 应用工作目录，不是 App 私有 data/config 路径。两个服务必须确实访问同一目录。App冻结目录/文件使用0700/0600，执行容器与App须映射为同一个宿主UID（仅匹配组不够）；常规Docker令VIDEO_UID/VIDEO_GID与RunDesk服务一致，rootless环境核对实际映射；执行交换探测后才能宣称共享可用。交换协议使用相对路径，所以 App 的 `/exchange` 与 Codex 工作目录绝对路径可以不同。

每次生产使用 `outputs/<sessionId>/runs/<productionRunId>/`：`input/` 是 App 导出的文本、配音、字幕、素材和关联快照；`work/` 保存 Codex 编辑；`artifacts/` 保存结果。App 验证 completion 后复制进私有归档，阶段卡片展示归档产物。旧运行的晚到结果不会自动成为当前版本。

## 输入和剪辑协议

`input/manifest.json` 由 App 导出，含 `schemaVersion:1`、`projectId`、`productionRunId`、`sourceHash`、`inputHash`、`media`、`narration`、可选 `subtitles`、`textPath`、`alignmentPath`。文件记录使用相对运行根目录的 `path`、`sha256`、`size`，素材含 `assetId`。Agent 不修改输入快照。

剪辑权威文件为自由编写的 `work/edl.json`，不与 App 原生 DSL 双写：

```json
{
  "schemaVersion": 1,
  "canvas": {"width": 1080, "height": 1920, "fps": 30},
  "clips": [
    {"id": "opening", "assetId": "a1", "path": "input/media/a1.png", "type": "image", "start": 0, "duration": 3, "fit": "cover", "textRefs": [{"quote": "发布了一款新芯片"}]},
    {"id": "detail", "assetId": "a2", "path": "input/media/a2.mp4", "type": "video", "start": 3, "duration": 4, "sourceIn": 1, "fit": "contain", "textRefs": [{"quote": "计划明年量产", "occurrence": 1}]}
  ],
  "subtitles": {"fontName": "Noto Sans CJK SC", "fontSize": 18, "marginV": 60},
  "notes": "自由记录剪辑意图；这里的示例总长七秒，实际必须服从真实配音时长。"
}
```

`clips[].textRefs` 是可选原文引用，可关联多段文字，不强制分句、分场景或一段文字对应一个镜头。每个镜头最多32条引用，每条 `quote` 必须逐字匹配冻结 `manifest.textPath` 指向的 UTF-8 原文，最多16384字节。原文重复时必须用1起算的 `occurrence`，重叠出现也计数。拒绝未知引用字段、null、空白引用、无效 Unicode 和控制字符（允许原文的换行、回车、制表符）；显式 occurrence 须为正整数。原文文件保持字节不变（包括 CRLF），其 SHA-256 必须等于 `manifest.sourceHash`。适配器在媒体探测前检查引用；传入 `textPath` 时即使无引用也校验原文。旧 EDL 无引用且旧输入不含 `textPath` 时仍兼容。保留已有镜头 ID 和无关引用；精修只改目标镜头，原文变化后使用新的冻结输入重新定位，不迁移旧引用冒充当前关联。

```bash
python3 /opt/rundesk-profile/tools/video_use_adapter.py render \
  --manifest RUN_ROOT/input/manifest.json \
  --edl RUN_ROOT/work/edl.json \
  --out RUN_ROOT/artifacts/render-001
```

适配器验证输入指纹和相对路径，统一图像/视频画布与帧率，为静音视频补无声音轨，实际调用上游 `extract_segment` 和 `concat_segments`，最后映射完整外部旁白并烧录已有 UTF-8 SRT。原片声音不混入旁白，切换画面不会切断 TTS，不依赖 Scribe/ASR。上游提取所需的临时 AAC 会在拼接前移除，避免许多短镜头累积音频编码填充并推迟画面切点。支持单轨连续图片与视频硬切、cover/contain；当前不支持平移缩放、叠加层、转场或背景音乐，未知 EDL 字段会报错。高级编辑可以继续使用 App 原生 DSL 后端。

图片/视频各镜头须连续覆盖配音全长，误差不超过一帧；视频 sourceIn+duration 不得超过真实片长；画布为 64..3840 的偶数像素，fps 1..60，总长不超过十分钟。JSON 和字幕有大小限制。路径不允许绝对路径、回退或符号链接；输出目录必须是新的 artifacts 子目录。输入经有根、禁止跟随符号链接的文件描述符复制并核对大小和 SHA-256，实际探测/解码读取私有副本；中间文件放在工作目录磁盘中，避开容器小容量 /tmp。探测与解码只接受独立媒体容器，拒绝播放列表/concat 外部引用，并关闭 MOV 数据引用。视频范围依据首个视频流时长或 Matroska/WebM 流 DURATION 标记，不以可能包含更长音轨的容器时长代替。

成功后生成 final.mp4、edl.json 快照、validation.json、probe.json 和三张采样帧，并原子创建运行根目录 completion.json。其 artifact 记录含 id、kind、path、sha256、size、mimeType；必须有 video/edit/validation 三种，probe/frame 可选。validation 同时绑定 schemaVersion、engine、upstreamCommit、projectId、productionRunId、sourceHash、inputHash、edlHash 和 narrationSHA256；要求非空检查列表全部通过。adapterVersion 为2；textReferences 记录原文是否验证、引用数、有引用镜头数、未映射镜头ID、源文本与引用并集的UTF-8字节数，以及每条引用的 refIndex（镜头引用数组中0起算的下标）、实际 occurrence/startByte/endByte；报告不重复保存quote，原文以EDL为准。覆盖统计仅说明声明的文字关联，不证明画面语义、新闻真实性或视觉质量，也不要求全文覆盖。edlHash 对应开始渲染前由已解析 EDL 写出的冻结快照，包含原样保存的引用；渲染期间修改 work/edl.json 不会改变本次工程归档。completion 已存在时拒绝覆盖；精修通过新生产运行保留版本。元数据和抽帧解码通过不等于 Codex 已看过图、听过音频或完成内容验收。

## 离线验收

```bash
python3 /opt/rundesk-profile/tools/profile_doctor.py --json
python3 -I /opt/rundesk-profile/tools/video_use_adapter.py --version
python3 /opt/rundesk-profile/tools/video_use_smoke.py --out ./adapter-smoke-001
```

doctor 不联网、不调用模型；核对来源 pin、适配器静态版本协议2、全部 Python 版本并真实合成两秒图片+静音模拟生成片段+连续 WAV+SRT，验证含CJK、emoji、CRLF、重复原文的多条引用，再检查 H.264/AAC、尺寸、帧率、时长、抽帧与完成清单。`--version` 无需输入文件，仅输出 schemaVersion、adapterVersion 和 upstreamCommit。这个烟测不证明真实 IndexTTS、生成服务、Codex 图片传递或端到端自主制作已验收。依赖解析及 CPython3.11/Linuxx86_64 二进制轮子选择、目标 Python 标记闭包验证记录在 dependency-validation.json；尚未运行完整 Docker 镜像安装与部署验收。
