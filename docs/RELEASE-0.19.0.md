# RunDesk 0.19.0 — Service lifecycle / 服务生命周期

- Linux/macOS native RPC processes use private process groups. Connection close, invalid protocol and parent exit trigger group cleanup. / 原生 RPC 进程组清理。
- Separate parent waiting from output reading; retain buffered frames while bounding inherited-pipe drains to one second. / 避免子进程占管道导致关闭挂起。
- Kernel-held data locks release after crashes. Legacy file locks are never silently overwritten. / 内核目录锁，保护旧格式锁。
- Administrator-only process snapshot UI/API, Chinese/English, desktop/mobile. / 进程快照界面与 API。
- systemd user-service example, explicit data/environment configuration. Runtime-manager failure exits nonzero so a service manager can restart it. / 用户级服务示例及故障退出码。

## Validation / 验证

Linux child-process fixtures cover close, parent exit, malformed JSON, descendant-held pipes, final-frame drain and unrelated-process preservation. Lock fixtures cover concurrent ownership, hard-kill release and legacy-lock rejection. Go tests and race checks; Linux executable hard-kill/restart smoke test; bilingual browser process view. Windows is cross-compiled only, not executed. No production systemd deployment, macOS runtime or real Docker cleanup was validated here.

## Boundaries / 边界

No Windows Job Objects, no universal detached-process cleanup, no remote-process cleanup. The registry covers App Server connections only. A SIGKILL of RunDesk requires OS service/cgroup supervision for descendants. Do not delete live lock files. Review legacy lock migration and local-filesystem assumptions in README. Restart does not replay uncertain tool actions.
