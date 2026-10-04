# 源码恢复记录

上一版持久文件下载长度 41,619,131 字节，缺少 ZIP 中央目录；原交付记录为 41,757,720 字节。重试下载得到同样结果。通过 ZIP 本地文件头恢复，验证解压后长度和 CRC32，提取 463 个完整条目。恢复以 scripts/generate-openapi.py 结束。所有生产源码、嵌入页面和 Go 测试均已恢复，并通过本版构建和测试。

scripts/images-smoke.cjs 从完整的旧开发补丁新增文件条目恢复。尾部历史脚本没有伪造或以空脚本替代。缺失的历史脚本包含 integration-smoke.cjs、native-smoke.py、process-model-test.cjs、product-smoke.cjs、queue-smoke.cjs、recovery-smoke.cjs、reply-actions-smoke.cjs、run.py、schedule-smoke.cjs、steer-smoke.cjs、trace-model-test.cjs、trace-native-mcp-smoke.py、trace-process-smoke.cjs、trace-smoke.cjs、ui-smoke.cjs、upgrade-smoke.py。旧文档对这些文件的引用属于历史记录。

0.11.0 提供完整的新源码检查点及新用户验收脚本，避免后续开发依赖不完整的历史 ZIP。
