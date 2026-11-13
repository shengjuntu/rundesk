# 验证范围与复验

RunDesk **0.73.0** 的本次变更为原生技能中心。实际执行结果、命令和限制见 [release-073.json](validation/current/release-073.json)。普通会话、应用预设和旧完整目录安装流程需保留回归；外部市场搜索与固定目录下载的联网验证，和离线协议测试分别记录。

SkillSpector 当前未在本环境安装实跑；其适配器使用官方报告协议 fixture 和模拟 CLI 验证。没有将内置规则结果代替 NVIDIA 实机结论，也没有进行真实 Agent、Docker 应用或目标 Windows 系统上的业务验收。Skill-Up 未纳入本次实现。

本次从 RunDesk 0.72.0 源包继续，原版验证记录保留在原源包；历史通过数不计作本版检查结果。现有镜像、配方、Kun SDK 与执行协议未做功能修改。配置协议替身、Node DOM 检查与交叉编译均有各自验证边界。

<a id="video-build-tmpfs-investigation-2026-11-08"></a>
## video-build 1.4.0 构建失败调查归档（2026-11-08）

以下为用户在部署服务器上完成的调查及对照验证，未在本次宿主开发环境中重新执行真实 Docker 构建或渲染。

| 阶段 | 用户报告的结果 |
| --- | --- |
| Docker 构建 | 成功；镜像为 `rundesk-video-build:0.157.1`，约 1.9 GB，所需工具已安装 |
| 离线工具检查 | `offlineReady: false`，最终构建记录标记 failed |
| Kun 修复 | 三次同版本重试仍失败，没有通过降级验收条件将镜像标成就绪 |
| 隔离对照 | 使用同一镜像和同一检查命令，仅将 `/tmp` tmpfs 改为 2 GiB 后，17/17 检查通过 |

旧版 `verifyRuntimeImage` 为 video-build 检查配置了 `size=768m` 的 `/tmp` tmpfs；虽然其内存额度较大，临时空间额度仍沿用了此值。用户观测到 HyperFrames render 预检返回 `Low disk space: 0.7 GB free at /tmp`，随后检查失败。上述同镜像对照支持将本次失败归因为宿主检查容器的临时空间配置，镜像无需仅因此重新构建。它不表明所有部署环境都必然以相同可用空间失败，也不将错误信息中的显示值当成完整磁盘度量。

用户报告通过的范围包括 Codex、Node、FFmpeg、GSAP、HyperFrames 0.8.87、字体、浏览器、渲染契约及 FFmpeg 音视频渲染。该 17/17 是用户实机结果，不等于本包已完成 Docker、模型认证、业务成片质量或全部预设的实机验收。本补丁保留上游工具和 doctor 检查条件，由宿主为检查提供足够临时空间；升级后仍须实际重跑检查，不能直接写入 `offlineReady`。

0.67.1 补丁的参数范围：宿主 `verifyRuntimeImage` 仅将 video-build 的 `/tmp` 提至 2 GiB，其余研究类 128 MiB、独立视频类 768 MiB 保持原值；构建中心通用 smoke 容器使用 2 GiB，以支持导入配方调用同一 doctor；Python CLI 的 `doctor` 仅将 video-build 从 512 MiB 提至 2 GiB，其他配方仍为 512 MiB。正式会话运行容器的配置不在本补丁范围，检查的其他隔离与资源限制保持原样。

<a id="kun-openmontage-trace-review-2026-11-08"></a>
## 另附 Kun／OpenMontage 会话轨迹审阅（2026-11-08）

用户附件 `rundesk-events(1).jsonl` 的 SHA-256 为 `c0d9c613e28252c7e6af69edb773fb639990379961196d62ae9a7a2d51407632`。它记录会话 `50bc82bc513520c8c6510a0c` 的 125 条宿主事件（#258–#382）。以下是对已保存证据的只读审阅，本源码不复制完整附件或原始载荷，也未据此在本机重新执行 Docker。

| 证据 | 已记录的事实 |
| --- | --- |
| 源构建 #298 | `profileId=openmontage`，OpenMontage 独立上游 1.0.0；构建 ID `ed77479dea01190ee85b3f3e`，镜像引用 `rundesk-openmontage:0.157.1`。失败发生在 `prepare archive upstream-source: context deadline exceeded`，没有生成 ImageID |
| 下载日志 #318 | 已读日志的下载进度仅到 32 MiB，不能据此认定归档已完整下载或校验通过 |
| Kun 操作 | 查询文件、日志和下载源列表；该附件未记录 `build_start`、配方修改、打包、导入或候选测试 |
| 工具别名错误 #360 | 此后模型调用继续，不是最终终止事件 |
| 终止 #380–#382 | 第十次模型请求于北京时间约 16:12:53 开始，约 120 秒后 #381 报 `context deadline exceeded (Client.Timeout or context cancellation while reading body)`，随后 #382 记录 failed。日志对应时间为 08:12:53–08:14:53 UTC |

该附件对应另一条 OpenMontage 下载失败修复会话，不能作为 video-build `/tmp` 空间根因或三次构建重试的证据。上节 video-build 的 17/17 对照结果来自用户单独提供的实机调查。0.67.1 补丁仅修正已明确的检查临时空间配置，不调整独立 Kun 或模型请求超时；附件中的超时记录也不单独证明是哪一端或哪个网络环节造成了失败。

## 基础检查

从源码根目录执行：

```sh
make build
make test
make check
python3 scripts/check-docs.py
python3 scripts/check-source.py
python3 scripts/generate-openapi.py
python3 scripts/test_profiles.py
python3 scripts/test_upstream_profiles.py
```

`make test` 分别检查宿主与轻量 Kun SDK；`make check` 包含两者的 race 和 vet。部分真实独立进程检查需要可执行的 Kun 与本版宿主，未提供时必须报告跳过，不能计作通过：

```sh
RUNDESK_TEST_KUN=/absolute/path/to/kun \
RUNDESK_TEST_HOST=/absolute/path/to/rundesk \
go test ./... -count=1 -timeout=300s
```

应将 `RUNDESK_TEST_HOST` 指向刚从本版源码构建的程序。交叉编译与对应系统上的启动验收分别记录。

## 部署和客户端检查

```sh
python3 scripts/test_portable_package.py \
  --binary /absolute/path/to/rundesk \
  --output-dir /outside-checkout/results/portable
python3 scripts/test_uhp_client.py
python3 scripts/test_uhp_client_integration.py \
  --binary /absolute/path/to/rundesk \
  --output-dir /outside-checkout/results/uhp-client
```

可运行包应解压到源码目录之外，以不同工作目录启动，核对版本、旁置 Kun、外置配方、源列表、启动文件与初始化状态。UHP 客户端集成检查启动隔离的 `--demo` 服务；它不连接线上任务或批准真实工具。官方协议套件的固定版本、运行方法与能力缺口见 [UHP](UHP.md#验证与复现)。

自动生成的报告、日志与截图默认写入 `build/qa/<检查名称>/`；支持输出参数的脚本可指定源码树之外的目录。它们是可再生的本机结果，不进入发布源码。提交前通过源码检查，避免将缓存、构建上下文、日志或截图重新加入目录。

## 结果的适用范围

- 单元测试、协议模拟器和 Docker 进程替身可以验证授权、状态、幂等、失败处理、冻结输入及界面行为；它们不证明真实 Docker 镜像已构建或模型已成功执行。
- offline、smoke、镜像身份检查、模型认证及代表性业务任务分别验收。Kun 修复任务完成不等于候选镜像已验证，匿名 manifest 可读不等于完整镜像层下载成功。
- 模型发现、推理、函数调用与实际 Agent 任务分别判断；本地端口可达不代表 GPU、模型模态或工具调用满足任务需求。
- Windows 交叉编译不等于 Windows 本机运行。公网镜像上传、国内外下载源连通性和下载速度只以目标网络的实际记录为准。
- UHP Extended 与应用内专项分别报告，不据此声明 Full 兼容或未实现的工具轮次、输出 token 硬预算。

当前行为与待验收边界见[能力状态](STATUS.md)，部署操作见[启动与部署](SETUP.md)，升级和旧数据兼容见[升级说明](UPGRADING.md)。
