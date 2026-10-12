# Wigolo 的评估与适配

核对日期：2026-10-12。目标项目 https://github.com/KnockOutEZ/wigolo ，选用 npm 发布版本 **0.2.1**，不跟随 latest。本轮以官方源码/配置说明评估并实现可选构建配置；未在当前环境实际构建容器或完成实时搜索对比。

## 结论

适合用作 news2douyin 和 people-search 的可选 Web 检索层。它提供搜索、正文读取、结构化提取、站点抓取、缓存和相似页面查询，具有原文片段、引用标识及降级状态等输出，适合把检索证据纳入 RunDesk 轨迹。它不负责短视频合成或发布，也不能仅凭项目演示认定优于 Exa。

Agent-Reach 主要是第三方工具的安装和配置脚手架；Wigolo 是可直接调用的 MCP/REST 检索服务，两者职责不同。此配置直接连接 Wigolo MCP；可选 Agent-Reach 只作为辅助 CLI。

## 原理与适用范围

Wigolo 使用多搜索引擎适配器合并候选结果，进行去重、排序融合与本地重排；网页读取按需要从 HTTP 升级到无头浏览器；缓存及本地模型位于自己的数据目录。查询仍会发给搜索引擎，网页仍需请求目标站点，因此“local-first”不代表离线搜索或查询永不离开机器。

核心搜索/读取不需要另配 LLM。高级 research/agent 的合成能力可另接模型；本配置只开放检索类工具，由外层 Codex 做分析和写稿，避免默认开启第二套模型合成循环。工具的失败、陈旧缓存及不完整结果应原样保存，不能被当成核实成功。

## Docker 适配

1. 采用 RunDesk 自有 Node 22 / Debian 镜像构建，安装固定 `wigolo@0.2.1`，运行独立 stdio MCP 进程，不启动嵌套 Docker。
2. 构建时通过该包依赖的 Playwright 安装 Chromium 和系统库，浏览器位于只读的 `/opt/wigolo-browsers`。
3. `/usr/local/bin/rundesk-wigolo` 在运行时把缓存/配置指向挂载 HOME 的 `.wigolo`。没有匿名 VOLUME；不照搬上游镜像的 `/data` 卷，因为 RunDesk 有自己的挂载约定。
4. 预装 Skill 放在 `/etc/codex/skills`，MCP 配置通过 RunDesk seed 初始化。已有环境的配置需要显式更新，不依赖构建时 root 的个人配置。
5. Exa 仍在配置中，但 Wigolo 模式下关闭。需要切回时在应用 MCP 配置中启用 Exa、禁用 Wigolo；不自动更换失败调用的来源。
6. 搜索缓存设为 300 秒、正文缓存 900 秒；新闻任务按工具当前 schema 使用 `force_refresh`。缓存状态、来源发布时间与事件时间都应保留。

上游说明初始安装需要约 1.5 GB 空间；浏览器、本地模型和缓存会继续占用磁盘。建议先配置 2 CPU、4 GiB 内存，并根据实际并发调整。模型首次下载和公网访问仍依赖网络；部署在数据中心不保证所有站点可读。

## 验收方法

生成配置、构建、运行离线 doctor 后，继续检查 MCP 初始化和工具列表；执行一条搜索及一条网页读取，保留原始响应。冷启动还需确认模型可下载、重启后缓存不丢失及非 root/只读根目录下无权限错误。

用同一批真实新闻选题对比 Exa 与 Wigolo，记录可用来源数、原始来源比例、时间窗口正确率、正文读取成功率、中文覆盖、耗时和内存。两条路线分别保存证据，不把自带相关性分数等同于事实可信度。当前尚无本项目实测支持“显著优于 Exa”的结论。

## 许可与参考

Wigolo 包声明 AGPL-3.0-only；官方另提供商业许可。它不会因被 RunDesk 调用而变成 MIT 组件。分发包含它的镜像时，应保留对应许可、来源与适用的源码提供安排；若需不同条款，可按上游渠道处理。此交付只包含独立编写的配置和适配脚本，没有复制 Wigolo 源码或分发其二进制。

- [项目介绍](https://github.com/KnockOutEZ/wigolo)
- [官方 Dockerfile](https://github.com/KnockOutEZ/wigolo/blob/main/Dockerfile)
- [配置与缓存说明](https://github.com/KnockOutEZ/wigolo/blob/main/docs/configuration.md)
- [版本与依赖](https://github.com/KnockOutEZ/wigolo/blob/main/package.json)
- [许可说明](https://github.com/KnockOutEZ/wigolo/blob/main/LICENSING.md)
- [Codex MCP](https://developers.openai.com/codex/mcp/)
- [Codex Skills 路径](https://developers.openai.com/codex/skills/)
