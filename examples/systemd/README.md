# RunDesk user service / 用户级服务

This example manages the entire service cgroup on Linux. It does not install or update Codex, Docker or model credentials. / 此示例让 Linux 服务管理器管理整个服务 cgroup，不自动安装 Codex、Docker 或模型凭据。

1. Copy the Linux binary to `~/.local/bin/rundesk` and make it executable. / 放置 Linux 程序并赋予执行权限。
2. Copy `rundesk.service` to `~/.config/systemd/user/rundesk.service`. **Edit `--data` to your existing data directory before upgrading.** The template default is for new installs. / 升级时必须改为原数据目录，模板默认值只用于新安装。
3. Optionally copy `environment.example` to `~/.config/rundesk/environment`, mode `0600`. Set an absolute PATH that finds Codex and its Node runtime. A user service does not source your interactive shell profile. / 配置可找到 Codex 和 Node 的绝对 PATH；服务不会读取交互式 shell 配置。
4. Stop any manually launched RunDesk first, then run / 先停止手工启动的旧服务，再执行：

```bash
systemctl --user daemon-reload
systemctl --user enable --now rundesk
systemctl --user status rundesk
journalctl --user -u rundesk -n 100 --no-pager
```

Stop/restart / 停止与重启：

```bash
systemctl --user stop rundesk
systemctl --user restart rundesk
```

For operation after logout, an administrator may enable lingering for your user (`loginctl enable-linger USER`). Availability and policy depend on the host. Do not run the service as root merely to avoid configuration. / 如需退出登录后运行，由管理员按主机策略开启用户 linger，不要为了绕过配置直接用 root。

The listener remains loopback-only; use an authenticated reverse proxy and configure RunDesk's public URL/token separately for remote access. Docker socket access must already be granted to the service user if Docker is used. / 默认仅监听本机；远程访问另行配置反向代理、公网来源和令牌，Docker 访问权限也需由管理员配置。

`KillMode=control-group` terminates processes still in the service cgroup on stop, including those that left their original process group. It cannot manage externally hosted services or processes moved to another cgroup. Kernel locks release on exit; **do not delete a live `server.lock` file**. For a leftover lock created by v0.18 or earlier, stop all old processes and remove that legacy file once. v0.19's persistent lock file is normal and should remain.

Upstream references: https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html and https://www.freedesktop.org/software/systemd/man/latest/systemd.kill.html
