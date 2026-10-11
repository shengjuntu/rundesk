# RunDesk 0.42.0

本版补齐 K4 首批产物质量检查：固定案例可声明文件存在、文本包含/相等、SHA-256；每案例最多 8 条，每个不同文件最多 1 MiB。Kun 在成功结束时固化判定与摘要，评分查询不读当前文件。

回复和产物共同决定来源采纳、独立验证晋升及重复对照质量。缺失证据保留未知；错误或缺失文件可阻断“回复正确”的候选。任务集界面支持增删、查看和复制产物规则，既有 API 增加可选字段，无新增路由。OpenAPI 仍为 180 paths / 216 operations。

升级两个程序：Kun 0.14.0、worker v14、fork schema 3。旧定义/报告指纹兼容；旧预览、检查点和候选需按引擎版本边界重新生成/验证。go.mod/go.sum 不变。文件观察不证明生成来源，不提供并发隔离或外部副作用裁判。

验收结果和日志见 [KUN-VALIDATION.md](KUN-VALIDATION.md)。实现范围见 [产物规则](KUN-ARTIFACTS.md)，阶段对齐见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。
