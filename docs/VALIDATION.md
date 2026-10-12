# 当前版本验证

RunDesk **0.46.0** / Kun **0.16.0** / worker 协议 **16**。2026-10-12 完成典型配置的本地验证。当前环境没有 Docker CLI、Docker Engine 或 Codex；本版交付的是配置生成器与源码，尚无实际构建并验收的镜像。

| 检查 | 结果与范围 |
| --- | --- |
| 配置契约 | 10 项测试通过；覆盖三套目录入口、九种组合、TOML 解析、固定版本、可重复 ZIP、拒绝覆盖、技能文件白名单、错误镜像和超时清理 |
| Skill 结构 | 三个项目内 Skill 的 frontmatter 与目录验证通过；没有进行模型行为验收 |
| 来源证据工具 | fixture 验证正文、摘要、失败记录与禁止覆盖；未实际请求 Jina 或公网 RSS |
| Go 兼容性 | containerruntime 与 app 的 Seed、Docker、Image、Build、Environment、V1、Setup 相关测试通过；Docker 测试使用 fixture |
| Linux 构建 | RunDesk 与 Kun 均通过；版本输出分别为 0.46.0 与 0.16.0 |
| 媒体实跑 | 宿主 FFmpeg 生成两秒 360×640 H.264/AAC 视频，ffprobe 核对编码、尺寸及长度；没有在容器内运行 |
| 语法与文档 | 新增 Python AST、Shell 语法通过；文档链接、OpenAPI 版本、启动管理权限及安装幂等约束通过 |

汇总见 [summary.json](validation/current/summary.json)，原始记录见 [配置测试](validation/current/profiles.log)、[Go 兼容性](validation/current/go-compatibility.log)、[Skill 验证](validation/current/skills.log)。实际媒体产物：[视频](validation/current/video-smoke-video.mp4)、[生成回执](validation/current/video-smoke-render.json)、[ffprobe 回执](validation/current/video-smoke-probe.json)。

本轮没有修改 Kun 内核或宿主业务行为；internal 内仅更新 RunDesk 版本常量与生成的 OpenAPI 版本。未重跑 0.45.0 的全量 Kun、race、DOM、HTTP 与 Windows 套件，不将旧版日志作为本轮验收。0.45.0 的完整验证保留在其独立历史交付中。

## 仍需部署验收

- Docker 实际拉取依赖、构建镜像、非 root 与只读根目录运行。
- Wigolo/Exa MCP 握手、工具发现、实时搜索、中文新闻覆盖及检索质量对比。
- Wigolo 模型初次下载、Chromium 运行、持久缓存权限与资源占用。
- Codex 认证、实际模型任务及 `/etc/codex/skills` 中技能的发现。
- 可选 Agent-Reach 安装和平台账号接入；新配置选择器的 WebUI 集成。

`doctor` 只报告检查过的范围；当前宿主返回 Docker 不可用是预期失败，见 [host-doctor.json](validation/current/host-doctor.json)。生成的 profile.lock.json 中 imageBuilt、modelTested、networkTested 均为 false，不因生成配置而宣称部署成功。

## 复验入口

```bash
python3 scripts/test_profiles.py
python3 scripts/check-docs.py
go test ./internal/containerruntime ./internal/app -run 'Test(Seed|Docker|Image|Build|Environment|V1|Setup)' -count=1 -timeout=180s
go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk
go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun
python3 examples/_shared/video_smoke.py --output /path/to/new-output
```

CLI 要求 Python 3.10+；契约测试使用标准库 tomllib，要求 Python 3.11+。Go 验证使用 1.25.12。媒体测试需要 FFmpeg/ffprobe。完整 Kun 套件仍可通过 scripts/validate-kun.py 运行，但不是本版验证范围。

最终源码指纹：`1551576e00ec1d96a3ed1bb790affe3f1561de0ded391900c298e183e3ec499d`。算法按相对路径排序，将 UTF-8 路径、NUL、原文件字节、NUL 依次送入 SHA-256；覆盖 Makefile、go.mod、go.sum、cmd、internal、scripts、examples，排除 Python 缓存。分发文件逐项摘要见根目录 SHA256SUMS。
