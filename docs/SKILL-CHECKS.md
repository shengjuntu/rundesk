# Skill-Up 效果测试与版本对比

RunDesk 的技能详情页提供“效果测试（Skill-Up）”：校验固定用例、单独运行测试、查看逐用例结果、导入已有报告，以及选择另一版本的报告作基线。静态安全检查继续见[技能中心](SKILL-CENTER.md)。测试结果不替代安全报告，也不会自动安装、修复或发布技能。

## 使用流程

1. 在技能中心获取包含 `SKILL.md` 和 `evals/eval.yaml` 的完整目录。Skill-Up 的用例、引用文件和脚本应随该目录提供；缺少固定入口时明确显示“用例未配置”。
2. 打开技能详情的“效果测试”，选择已有的受管 Docker 制作环境。镜像内必须包含 Skill-Up 及用例声明的 Agent、工具和模型配置。
3. 阅读并确认执行说明，再点击“校验用例”。此操作解析 YAML 及其文件引用，也在独立容器中运行。退出码为 0 但没有明确的非空用例完成输出，不会被当作成功。
4. 需要实测时，单独点击“运行测试”。网络默认关闭；确需访问模型或其他服务时显式允许网络。该操作会运行用例配置的 Agent、工具、脚本和 Judge。
5. 查看任务状态、报告及每个用例的 `PASS / FAIL / ERROR / SKIP / UNKNOWN`。`completed` 只说明取得了有效校验结果或报告，仍须阅读报告结论；它不表示所有用例通过。
6. 选择基线与候选报告。基线可以来自另一个技能快照，候选必须属于当前快照。查看逐用例差异及整体可比性说明。MVP 的自动回归门禁只支持根 `SKILL.md` 修订，辅助文件和执行身份保持一致，且使用可核对的内建规则；不能核实可比性时保留为未知，不标成修复或退化。

报告列表返回摘要，选择报告后才加载完整用例。`with_skill` 与 `without_skill` 分别展示；主汇总按用例选择主结果，不把两种变体当成两个独立主用例。文件上传导入的上限为 4 MiB、1000 个用例；仅有空对象、无法识别的状态或缺失结果不能生成完整通过。

## 执行边界

当前实际执行要求运行 RunDesk 的主机为 **Linux，并能连接本机 Docker Engine**。环境列表的 `available` 仅反映 Docker 连接和已有配置候选，不证明镜像内工具、模型或用例已经就绪。没有可用 Docker、镜像或固定用例入口时返回明确错误，不会退回宿主直接执行 YAML 或 Agent。

每个任务冻结技能内容摘要、辅助文件摘要（`suiteDigest`）及本机不可变镜像 ID，使用独立容器。辅助文件摘要覆盖冻结包内除根 `SKILL.md` 外的全部文件路径、字节和执行位，包括 `evals/` 内外的用例、脚本及数据。容器的来源目录只读，不挂载日常工作区、主机 Codex HOME、主机凭据目录或 Docker socket；不沿用日常容器的权限。根文件系统只读，能力全部移除，启用 `no-new-privileges`，以非 root 用户运行；CPU、内存、进程和任务时限均受限。工作、临时及输出目录使用限额 tmpfs。镜像沿用现有准入检查：必须是与主机架构匹配的 Linux 镜像，并拒绝声明 `VOLUME` 的镜像，避免继承额外可写匿名卷。

网络默认是 `none`；明确允许后仅改为 `bridge`，不使用宿主网络。镜像自行包含的配置仍属于该测试环境，RunDesk 不自动给它复制主机账号凭据，也不自动安装或登录模型服务。已有 Codex 镜像不等于模型认证已经就绪。

固定执行命令为：

```text
/usr/local/bin/skill-up validate /source/evals/eval.yaml
/usr/local/bin/skill-up run /source/evals/eval.yaml --output-dir /output --iteration 1 --parallelism 1
```

管理员可在 RunDesk 服务进程设置 `RUNDESK_SKILLUP_CONTAINER_BIN`，覆盖**容器内**的可信绝对命令路径；页面不接受任意命令。它不是宿主 `skill-up` 的安装路径。运行输出是 `/output/iteration-1/result.json`，不是把 stdout 当作 JSON 报告；取回时只接受单个有界普通文件，不在宿主展开容器提供的链接或目录。

任务支持取消。取消、超时和执行结束都会尝试清理该任务拥有的容器。接受取消到清理完成之间，`cleanupPending` 仍为 `true`，执行槽继续占用；若终态的 `cleanupError` 非空，页面会提示并提供重试清理。在清理未确认前继续阻断新的测试，不能将“已收到取消”理解为环境已空闲。宿主重启把在途任务标为中断并尝试清理，不自动重跑测试；遗留清理失败仍会保留阻断。

## 准备包含 Skill-Up 的镜像

本版协议核对使用 [Alibaba Skill-Up v0.15.0](https://github.com/alibaba/skill-up/releases/tag/v0.15.0)，固定源码提交为 `ffba9031129271260026dbb2a823af23d0505100`。RunDesk 不合并其 Go 源码；上游该版要求 Go 1.26，独立官方二进制可放入测试镜像，不改变 RunDesk 的 Go 构建要求。

以下仅为 **Linux amd64** 镜像构建片段。以你已批准、包含所需 Agent 与下载工具的基础镜像为基础，将基础镜像固定到实际 digest；其他架构必须选择对应官方资产并核对其校验值。此示例没有在当前环境进行真实 Docker 构建验收。

```dockerfile
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
USER root
RUN curl -fL \
      https://github.com/alibaba/skill-up/releases/download/v0.15.0/skill-up_0.15.0_linux_amd64.tar.gz \
      -o /tmp/skill-up.tar.gz \
    && echo 'd5f0591d8a357816cc2c2d5b7303f89caabfe7297bc2f85123631e5cbf40747f  /tmp/skill-up.tar.gz' | sha256sum -c - \
    && tar -xzf /tmp/skill-up.tar.gz -C /usr/local/bin skill-up \
    && chmod 0755 /usr/local/bin/skill-up \
    && rm /tmp/skill-up.tar.gz
```

该发布资产为 23,113,774 字节；上述 SHA-256 已与同一发布的官方校验文件核对。选择或构建完成镜像后，在 RunDesk 配置为受管 Docker 制作环境。运行会固定已存在的本机镜像 ID，不自动拉取同名新标签。

用例中的引擎配置决定使用 Codex、Claude Code、Qoder 或自定义引擎。对应可执行文件、支持版本、模型访问方式和 Judge 依赖需要在测试环境中准备；本版不会把“发现了环境”标为“多引擎实测已通过”。

## 导入报告与可比性

可以导入上游原始 `result.json`。必须显式确认该报告的执行来源未核验，报告记录标记为 `external_unverified`。其技能摘要关联是用户声明，不能证明原始运行实际使用了该技能、用例或模型；原始 JSON 中自带的用例摘要不能自证可信。

RunDesk 只展示有界、脱敏的规范化字段，不直接返回原始 prompt、response、完整配置或任意未知载荷。配置和输入身份以摘要参与核对；上游明确提供逐用例的 `observed_model` 或 `observed_version` 时，另以 `runtimeId` 摘要核对，运行身份变化不会忽略。模型／配置／输入未知或变化、缺少可信辅助文件摘要、辅助文件或用例规则改变、镜像改变、存在新增／删除／跳过／未知项时，不能形成回归通过。包含外部导入来源的对比也保持 `incomplete`。

本版自动回归门禁只支持**纯根 `SKILL.md` 修订**：冻结包内所有其他文件、运行镜像、模型、配置与声明输入身份都须相同；只在可核对的内建规则 `grading/gates` 证据下，才可判断失败转为通过、退化或回归通过。修改辅助脚本、数据或其他文件后，仍可分别运行并查看两份报告，但对比明细统一保留 `unknown`，整体为 `incomplete`。

脚本、Agent 或未知 Judge 的实现身份无法完整证实时，即使报告中各自观测到 `PASS`，也不能用它建立回归门禁通过；对比同样保持全部 `unknown / incomplete`。`suiteDigest` 涵盖 `evals/` 外的包内文件，可阻止辅助脚本或数据变化被静默当作规则未变，但不会把无法证实的 Judge 执行升级为可比证据。逐报告结果仍可查看；这些证据不单独证明业务收益、因果关系或总体效果。

效果报告与静态安全报告分别保存。`PASS` 不会让安全扫描的风险项消失，外部导入报告也不会给安装门禁提供安全通过。

## 管理 API

以下路径以 `/api/v1` 为前缀，均沿用管理员认证，不开放给普通成员或应用凭据。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /skill-center/check-engines` | Docker 可用性与已配置环境候选 |
| `GET /skill-center/packs/{id}/checks` | 当前快照的测试任务 |
| `POST /skill-center/packs/{id}/checks` | 创建校验或运行任务，返回 202 与任务 |
| `GET /skill-center/check-jobs/{id}` | 查询任务、退出码、校验结果、报告编号及清理问题 |
| `POST /skill-center/check-jobs/{id}/cancel` | 取消，或重试清理遗留的该任务容器 |
| `GET /skill-center/packs/{id}/check-reports` | 当前快照报告摘要 |
| `POST /skill-center/packs/{id}/check-reports` | 导入外部报告并标明未核验来源 |
| `GET /skill-center/check-reports` | 跨快照基线候选摘要 |
| `GET /skill-center/check-reports/{id}` | 单份完整规范化报告 |
| `GET /skill-center/packs/{id}/check-compare?baseline=…&candidate=…` | 对比基线与当前候选 |

创建任务字段为 `digest`、`action`（`validate` 或 `run`）、`instanceId`、`allowNetwork` 和 `acknowledgeExecution`。导入字段为 `digest`、`report`（上游 JSON 对象）和 `acknowledgeImport`。接口不承诺幂等重放；提交结果不确定时先查询已有任务，不应自动重复执行。

## 实际验证边界

本次开发环境没有 Docker。容器执行参数、隔离、报告取回、取消和清理行为使用协议替身与 fake driver 验证，未作为真实 Docker 或真实模型运行验收。

官方 v0.15.0 二进制经过发布校验值验证，并以官方受控 custom-engine mock 跑过 `--version`、`validate` 和 `run`，取得实际 `result.json`。mock 只返回固定响应，验证的是 CLI、会话结果和报告协议；未使用 LLM，也不代表目标 Agent 的效果。完整最终检查与限制以[本版验证记录](VALIDATION.md)为准。
