# RunDesk 0.18.0 — Observed usage / 运行用量

Tasks → Usage / 任务 → 用量统计：日期、应用、项目筛选，累计 token 的已观测增量，输入／输出、执行轮次与缺失用量覆盖情况，JSON 快照导出。中英文与移动端适配。

`GET /api/v1/usage` is administrator-only. No model call is made; the query projects only counter and lifecycle metadata from the journal. Thread snapshots are deduplicated; unknown baselines are not charged as new work. Regressions are flagged instead of reset-and-add. Missing fields remain null. Cached/reasoning subsets are not added again. Limits: 10 seconds / 200,000 relevant events; no silent truncation.

本版没有新增硬性限额、货币计费、跨租户资源隔离或长期独立账本。删除会话也会删除相应统计。缺失上报和跨期间事件影响完整性／期间归属，详情见 README。

Validation: synthetic cumulative/duplicate/regression/missing/baseline/boundary records; administrator API boundary; full Go tests; browser checks with explicit demo protocol events. No real provider billing reconciliation was performed.

Upgrade: back up data, stop the previous binary, replace it and retain the same `--data` directory. Startup adds an event-method index; large journals may take longer on first start.

Counter semantics reference: https://github.com/openai/symphony/blob/main/elixir/docs/token_accounting.md
