# Kun 0.13 / RunDesk 0.41.0 验证记录

验证日期：2026-10-09（北京时间；原始日志使用 UTC）。Go 1.25.12；Kun 0.13.0 / worker v13 / fork schema 3。最终八组发布回归全部退出 0，交付源码与本次回归的源码指纹一致。

| 项目 | 已确认结果 |
| --- | --- |
| 全量 Go | 全包通过，84.25s |
| 静态检查 | go vet 全包通过 |
| 专项竞态 | app、kun、store：案例/Skill/快速路径/对照/事务/容量/兼容检查通过，13.257s |
| 构建 | Linux 两程序构建与 CLI、hello/EOF、协议 12 拒绝通过；Windows 两程序交叉编译通过 |
| JS 与界面 | 83 个 JS/CJS 语法检查、12 组 Node DOM 交互通过 |
| 既有 HTTP | 实际宿主 + Kun 子进程 + 本地 Provider 夹具；任务集、Hybrid、假设与评分链路通过，6 次模型调用 |
| K4 HTTP | 来源→候选→独立验证→启用/重启/新输入→停用，以及普通对照 8 次、回退对照 20 次运行通过；总计 40 次模型调用 |
| API 契约 | 180 paths / 216 operations；新增 8 个对照操作；引用、参数唯一性、管理员边界及必需幂等头通过 |
| 兼容 | go.mod/go.sum 不变；worker/IR 版本不变；0.40 独立验证报告指纹保持有效 |

## 多次采样验收

| 行为 | 证据与结果 |
| --- | --- |
| 固定且原子保存 | 全部计划与批次单事务；插入冲突不留下部分案例；独立 plan/session/run；2–5 次、最多 100 次界限 |
| 预览不执行 | 未创建 worker/会话、零模型调用；重复 Key 返回相同预览；每项 full preview 绑定批次 |
| 顺序与授权 | 交替先后；一次整批确认；禁止单案例 start 绕过批次；成员和应用 key 在 v1/兼容/member 路径都拒绝 |
| 容量与停止 | 已占用共享名额时等待；全局仅一个对照调度；停止返回后不接收新案例，在途运行继续 |
| 重启与失败 | 停止后重启、显式继续跳过完成样本；中断状态 GET 不启动 worker；失败/中断/删除不重跑；环境漂移在执行前阻断 |
| 长批次 | 20 次真实进程运行连续完成，完成样本释放自己的 worker，未扩大 16 个连接名额 |
| 未知与回退 | 未运行报告通过率/总用量为 null；指标 count/missing 分列；回退成功仍算质量通过，回退单列 |
| 报告与门槛 | 每次游标/事件摘要、规则/回复证据固定；旧报告不覆盖；对照报告不能作为独立验证晋升凭据 |
| 工作台 | 不自动执行，重复点击受控，整批确认和指纹、逐例查看、报告身份验证、未知显示、关闭后忽略迟到响应 |

本地夹具中的普通批次有 4 个配对：动态/快速均 4/4 通过，模型调用合计为 8/0，配对平均差为 −2 次；回退批次有 10 个配对，快速组 10/10 完成、10 次回退、10 次模型调用，每次快路径已完成写入未再次派发。这验证计量与回退机制，不是远程业务性能或质量收益证据。毫秒数据保留在 JSON 中，不用于此夹具的速度宣称。

Go 专项测试：`TestKunBenchmarkFixedPlansUnknownAndRestart`、`TestKunBenchmarkBoundsDriftAndFailedSample`、`TestKunBenchmarkMetricsMissingFallbackAndFailed`、`TestKunBenchmarkWaitsForSharedCapacityAndStop`、`TestKunBenchmarkPreservesLegacySkillReportHashes`、`TestKunBenchmarkAtomicPlanCreation`。既有 K4 和其他回归同时保留。

## 原始记录与复验

- [最终八组汇总](validation/kun-0.41.0-final2/summary.json)
- [Go](validation/kun-0.41.0-final2/go-test.log)、[vet](validation/kun-0.41.0-final2/go-vet.log)、[race](validation/kun-0.41.0-final2/go-race.log)
- [Linux](validation/kun-0.41.0-final2/build.log)、[Windows](validation/kun-0.41.0-final2/windows.log)
- [语法](validation/kun-0.41.0-final2/syntax.log)、[界面](validation/kun-0.41.0-final2/ui.log)、[HTTP](validation/kun-0.41.0-final2/http.log)
- [对照 HTTP 计量](kun-benchmarks-http-validation.json)、[对照 UI](kun-benchmarks-ui-validation.json)、[K4 HTTP](kun-k4-http-validation.json)
- [CLI](kun-cli-validation.json)、[契约](kun-contract-validation.json)、[构建](kun-build-validation.json)

早期集成检查保留于 `validation/kun-0.41.0-integration/`。两次主动中断的检查保留于 `kun-0.41.0-final/` 和 `kun-0.41.0-release/`：先修正兼容测试中 map/struct 的序列化比较，再补长批次 worker 回收与 20 次运行。中断结果不计为本版发布通过；最终以 `kun-0.41.0-final2/` 为准。0.40 的既有证据另行保留。

复验：`python3 scripts/validate-kun.py --output validation/kun-local-1`，Go 不在 PATH 时加 `--go /absolute/path/to/go`。需要 Python 3、Node、go.mod 指定 Go 和 race 所需 C 编译器；每次使用新目录。HTTP 必须依赖同次成功构建；源码指纹覆盖 cmd/internal/scripts、go.mod/go.sum/Makefile，不含文档及生成记录。

最终源码 SHA256：`47f9d1ce6e5fd8a7b877d6a78d9ef335296676c9275a50640edc25ca9eb5bb95`。

## 范围

真实浏览器布局/焦点/移动端、真实 Codex/远程模型/MCP、代表性业务任务集、Windows 执行、生产负载仍需对应环境。没有随机试验、统计显著性、外部状态隔离/重置、货币费用估算或事实/产物裁判。学习型 JEV 未训练或接入。Node DOM 与本地夹具不代替这些验收。
