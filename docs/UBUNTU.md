# RunDesk v0.5.4 · Ubuntu 安装与升级

适用：Ubuntu Linux x86-64，单用户运行。安装包提供 `bin/rundesk`，已内嵌 WebUI；运行 RunDesk 无需 Go、Node.js 或独立数据库。Codex CLI 是单独安装的组件。

## 已经运行 v0.2 / v0.3 / v0.4 / v0.5.0：优先原地升级

1. 记录旧服务的 `--data`、`--codex`、`--listen`、`--public-url` 以及 CODEX_HOME、访问令牌和系统用户。
2. 停止旧后台。若使用用户级 systemd：`systemctl --user stop rundesk`；若在终端运行，按 Ctrl+C 并等待退出。
3. 在停止状态下备份原数据目录、外部工作区和 CODEX_HOME。不要只复制 state.db 而忽略可能存在的 WAL/SHM。
4. 解压新包到单独的程序目录，验证版本，再替换服务使用的二进制。保持旧数据路径、CODEX_HOME 和工作区路径。
5. 重新启动，打开页面确认 0.5.4。v0.2 会话归入默认实例，v0.3 / v0.4 会话保留原实例绑定。运行中的旧任务不会自动续跑，需再次发送消息。

```bash
unzip rundesk-v0.5.4.zip -d ~/rundesk-v0.5.4
cd ~/rundesk-v0.5.4/rundesk
chmod +x bin/rundesk
./bin/rundesk --version
sha256sum -c SHA256SUMS

# 示例：将路径替换成旧服务实际使用的目录
./bin/rundesk --data /home/YOUR_USER/.local/share/rundesk
```

最后一条是手动启动示例，systemd 用户不要同时运行它。若之前没有传 `--data`，继续保持原命令和同一个用户即可。回退请停服务并恢复升级前备份；不要用旧版本打开包含新实例会话的数据。

## 新安装

先确认 `uname -m` 为 `x86_64`；ARM64 需要从源码构建对应平台。若缺少 unzip，可先 `sudo apt install unzip`。

先安装 Codex CLI。本包已验证版本为 0.157.1；若已有合适的 Node.js/npm 环境，也可固定版本安装：

```bash
npm install -g @openai/codex@0.157.1
codex --version
codex login
codex login status
```

不要使用 sudo 登录 Codex；登录和运行 RunDesk 应是同一个系统用户。

解压后执行：

```bash
./bin/rundesk --listen 127.0.0.1:3210 --data "$HOME/.local/share/rundesk"
```

浏览器打开 `http://127.0.0.1:3210`。程序找不到 codex 时，使用 `command -v codex` 获取路径，再传 `--codex /绝对路径/codex`。

不调用模型的演示：`./bin/rundesk --demo`。演示数据库和配置使用独立 demo 子目录。

## 用户级 systemd 后台

此示例假定二进制安装在 `~/.local/bin/rundesk`，数据为 `~/.local/share/rundesk`，默认 Codex 状态为 `~/.codex`。现有部署请保留原路径，不要直接套用新的数据目录。

```bash
mkdir -p ~/.local/bin ~/.config/systemd/user
install -m 0755 bin/rundesk ~/.local/bin/rundesk
command -v codex
```

创建 `~/.config/systemd/user/rundesk.service`，把 `/ABSOLUTE/PATH/TO/codex` 替换为实际路径。如果 MCP 使用 npx、uvx 等命令，也必须使 systemd 的 PATH 能找到它们。

```ini
[Unit]
Description=RunDesk Codex runtime
After=network-online.target

[Service]
Type=simple
Environment=CODEX_HOME=%h/.codex
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=%h/.local/bin/rundesk --listen 127.0.0.1:3210 --data %h/.local/share/rundesk --codex /ABSOLUTE/PATH/TO/codex
Restart=on-failure
RestartSec=3
KillMode=control-group
TimeoutStopSec=20

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now rundesk
systemctl --user status rundesk
journalctl --user -u rundesk -n 100 --no-pager
```

需要退出系统登录后仍由用户服务管理器运行时，可按本机管理要求启用 linger：`sudo loginctl enable-linger "$USER"`。

更新已安装的二进制：先 `systemctl --user stop rundesk`，备份，再运行上面的 install 命令，最后 `systemctl --user start rundesk`。

## 创建第二个助手

1. WebUI「配置 → 实例 → 新建实例」，例如“视觉助手”。
2. 按页面显示的绝对目录登录：`env CODEX_HOME='/实际的实例目录' codex login`。
3. 点击“重新加载空闲连接”，再读取登录状态。默认实例的原有登录不会被自动复制。
4. Skills 页选择“实例技能”，MCP 页添加该助手的服务；选择工作区后发起新对话。

项目技能和文件属于工作区，共享该目录的实例能看到它们；全局技能、环境变量和系统密钥环也可能共享。独立 CODEX_HOME 不是容器隔离。

## 常见问题

- **旧会话不见了**：先检查系统用户和 `--data` 是否与旧服务一致，再检查实例/工作区与归档筛选。
- **原生 thread 无法恢复**：检查默认 CODEX_HOME、服务用户和工作区绝对路径是否被更换。
- **Codex 未登录 / MCP 命令不存在**：systemd 不会读取交互 shell 的完整环境。检查对应实例账号与 PATH；不要把凭据粘贴进公共日志。
- **配置保存但效果不同**：看 MCP 的“有效配置与来源”，项目或托管层可能覆盖用户层；运行中的任务保持原状态，下一轮重载 MCP。
- **连接达到 16 个上限**：关闭闲置会话的连接可使用实例页“重新加载空闲连接”；也会在空闲十分钟后回收。
- **server.lock 遗留**：只有确认该数据目录没有任何 RunDesk 进程使用后，才删除锁文件。

远程访问和反向代理参见 README 的部署部分。升级后先运行“配置 → 运行配置 → 连接诊断”，再用你的本地账号做一次真实模型和 MCP 任务验收。


## 沙箱前置条件与本次故障修复

Ubuntu 安装系统 bubblewrap：

```bash
sudo apt update
sudo apt install bubblewrap
```

Ubuntu 24.04 若出现 `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted`，检查 AppArmor 的用户命名空间限制。官方建议的专用配置加载方式：

```bash
sudo apt install apparmor-profiles apparmor-utils
sudo install -m 0644 /usr/share/apparmor/extra-profiles/bwrap-userns-restrict /etc/apparmor.d/bwrap-userns-restrict
sudo apparmor_parser -r /etc/apparmor.d/bwrap-userns-restrict
```

安装或加载失败时保留错误输出；不要把此步骤放进每次启动服务的脚本。AppArmor 配置加载立即生效。apt 的 Pending kernel upgrade 是另一个维护提示，不证明沙箱错误必须靠重启解决。

以运行 RunDesk 的同一个普通用户、同一个 Codex 可执行文件测试；不要使用 sudo 执行 Codex：

```bash
codex --version
codex sandbox --help
# 本次已核对的 Codex 0.157.1 使用直接命令格式：
codex -c 'sandbox_mode="workspace-write"' sandbox -- /bin/echo sandbox-ok
```

旧版帮助若列出 `linux` 子命令，则使用 `codex ... sandbox linux -- /bin/echo sandbox-ok`。以本机帮助为准；v0.4 诊断会识别这两种格式。`Failed to execvp linux` 通常意味着将 linux 当成了待执行程序，需要核对 CLI 格式。

如果仍出现权限错误，在重试后读取：

```bash
sudo journalctl -k --since "5 minutes ago" --no-pager | grep -Ei 'apparmor|denied|bwrap|userns'
```

修复后重启 RunDesk，或对当前实例重新加载空闲连接；运行中的任务不会因此被重载。新建会话检查工作区普通读取。网络访问与越界操作仍遵循配置；沙箱通过不代表所有操作都不需要审批。

参考：[官方沙箱前置条件](https://developers.openai.com/codex/concepts/sandboxing#prerequisites)。RunDesk 不会自动安装系统包、修改 AppArmor、设置内核参数或关闭沙箱。

## 打开轨迹工作台

选择已有会话，点击顶栏「轨迹」。历史运行可以直接查看，不需要重新执行。先用「全部运行」看总览，再选择单次运行；时间轴支持滚轮缩放与拖动，上/下一个和异常导航固定在底部。详情可收起，手机使用抽屉。演示模式输入“轨迹 审批”可检查交互，所有结果均为模拟。
