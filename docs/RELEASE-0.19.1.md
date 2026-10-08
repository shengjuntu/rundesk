# RunDesk 0.19.1 — File access ownership / 文件访问归属

## Changes / 修改

File downloads previously checked workspace access without verifying which application owned the file. Applications sharing a workspace could request another application's file by path. Downloads now verify the output session's workspace, application and instance, or a persisted upload ownership record. Both API aliases and member routes use these checks.

此前下载接口仅校验工作目录权限，未校验文件所属应用。共享工作目录的应用可以按路径请求其他应用的文件。本版输出文件按会话的工作目录、应用与实例校验；上传文件按持久化归属记录校验。旧版和 v1 API、成员入口均覆盖。

- Upload ownership follows application + instance + workspace; key rotation preserves access. Members with the same application/project grant intentionally share these attached files.
- Task creation, schedule creation/update, turns and steer validate submitted file references before execution or idempotency replay.
- Stable symlink aliases are refused for scoped file access.
- 上传归属绑定应用、实例与工作目录，轮换密钥不丢失访问权。同一应用／项目授权的成员按现有协作规则共享这些附件。
- 任务、定时任务新增／修改、发送消息与追加指令，在执行或读取幂等回执前检查附件归属。
- 拒绝通过已有符号链接别名访问文件。

## Upgrade / 升级

No destructive migration is performed. Existing output files remain accessible if their owning session exists and is authorized. Legacy uploads without ownership records fail closed for application/member requests. Re-upload them through the authorized application's or member's upload endpoint. Administrators can still download them. Attaching a personal-library file through the member workspace upload endpoint grants the attached copy to that application/project, while the original personal library remains private.

不执行破坏性迁移。旧输出文件只要所属会话存在且有权访问，即可继续下载。没有归属记录的旧上传文件对应用／成员拒绝访问，请通过对应应用或成员上传入口重新上传；管理员仍可下载。成员将个人文件库的文件加入应用项目时，附件副本按该项目共享，原个人文件库仍为个人所有。

## Boundaries / 边界

This is API-level authorization, not physical tenant isolation. An agent or external process with direct write/read access to a shared filesystem is outside this boundary; symlink checks do not provide race-proof isolation from hostile writers or hard links. Use separate directories and container environments for untrusted applications. Already queued tasks, stored recovery input and existing running agents are not retroactively rewritten or canceled; review them before enabling untrusted callers. This patch adds no enterprise tenant model, per-user isolation within a shared grant, or hard resource quotas.

这是 API 层授权，不是物理租户隔离。能直接读写共享文件系统的 Agent 或外部进程不受此边界约束；符号链接检查不能防止恶意并发替换或硬链接。不可信应用应使用独立目录与容器环境。已经排队的任务、已有恢复输入和运行中的 Agent 不会被追溯重写或取消，开放给不可信调用方前应先检查。本版没有新增企业租户模型、同一共享授权内的个人隔离或硬性资源配额。

## Validation / 验证

Regression coverage includes two applications in one workspace, both API aliases, member runner/viewer grants, own and foreign outputs/uploads, legacy uploads, administrator access, key replacement, attachment submission and symlink aliases. Full Go tests and focused race tests are run for this patch. Linux and Windows binaries are built; Windows is cross-compiled, not runtime-tested. No UI changes or real Docker/model deployment claims.

回归覆盖同目录的两个应用、两套 API 路径、成员执行／只读授权、本应用与其他应用的输出及上传文件、旧附件、管理员访问、换密钥、附件提交与符号链接。执行全量 Go 测试与针对性竞态测试。构建 Linux 与 Windows 程序；Windows 为交叉编译，未做运行验证。本版无界面修改，未验证真实 Docker／模型部署。
