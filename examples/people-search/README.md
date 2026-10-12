# people-search

面向演讲者、研究者、作者、创始人等人物的公开职业资料研究。提供同名消歧、职业/作品时间线、来源核对与证据报告；不包含私人账号访问、私人联系方式挖掘或自动联系。

统一入口：`python3 examples/people-search/run.py <命令>`。例如 `inspect`、`validate`、`pack --codex-version X.Y.Z --output ./people.zip`。完整参数见 [配置目录说明](../README.md)。

默认 Exa，可用 `--search-backend wigolo` 切换；Jina 用于公共页面阅读。`--with-agent-reach` 为可选辅助工具，未配置的账号和渠道保持未配置。

附带 `research-public-people` Skill。输出 sources.json、people.json、report.md，保留 candidateId、身份置信判断、冲突和未知字段。示例任务：研究某位公开技术演讲者近期的工作与代表作，核对同名候选并列出官方职业资料来源。

搜索成功不等于身份匹配；验证时重点检查同名误合并、过期头衔、二次转载误当独立证据。
