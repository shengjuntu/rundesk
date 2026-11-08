# 验证范围与复验

本页说明当前源码的复验入口，不把历史通过数当作本版结果。RunDesk **0.67.0** 聚焦同主题文档／测试整理及配方共享输入。应复验共享来源物化、本地覆盖、缺失或不安全来源拒绝、冻结锁文件不依赖公共目录，以及历史上下文不变。

本版实际执行、结果及未覆盖范围只记在 [release-067.json](validation/current/release-067.json)。完整历史记录保留在 0.65.0 源码包，来源与摘要见[历史资料说明](validation/README.md)。本版不随源码再次携带历史日志、截图、测试视频或嵌套归档。

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
