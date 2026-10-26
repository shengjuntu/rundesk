# 历史验证归档

这里保存随原始源码包提供的旧发布报告、测试原始日志和阶段摘要。**归档不代表本次重跑，也不代表当前源码已通过相同范围的验收。** 当前入口见 [验证说明](../../VALIDATION.md)。

| 范围 | 入口 |
| --- | --- |
| 历史发布验证叙述 | [原发布记录](RELEASES.md) |
| 0.52.0 构建中心与插件 | [报告](validation-0.52.0.json) |
| 0.51.0 独立视频环境 | [报告](validation-0.51.0.json) |
| 0.50.0 运行预设 | [报告](validation-0.50.0.json) |
| 0.49.2–0.49.5 | [0.49.2](validation-0.49.2.json)、[0.49.3](validation-0.49.3.json)、[0.49.4](validation-0.49.4.json)、[0.49.5](validation-0.49.5.json) |
| 0.49.1 会话采集开关 | [检查](validation/current/0.49.1/checks.json) |
| 0.49.0 统一调试 | [说明](validation/current/0.49.0/VALIDATION.md) |
| 更早的 0.47.1 / 0.48.0 | [0.47.1 摘要](validation/current/summary.json)、[初始化记录](validation/current/runtime-init.json) |

为保留证据引用关系，归档中的目录结构沿用原 `docs/` 下的相对布局。此处的 `validation/current/` 是**原包路径**，并不表示当前版本。原 JSON、JSONL 和日志内容不改写，包括测试时间、失败、跳过项、旧源码路径及当时限制；报告中的源码清单可能指向随后已合并的旧文档。

例如，`validation-0.52.0.json` 中的 `tests-0.52.0.jsonl` 和 `validation/current/0.52.0/…` 均在本归档内保持对应关系。原发布说明的 Markdown 链接已调整到归档位置。失败、首次尝试和最终通过日志都保留，不能只选择通过记录解释测试结果。
