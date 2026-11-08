# Python clients / Python 客户端

These clients use Python 3.10+ and its standard library. They are application
integration references, not dependencies of the RunDesk server or portable package.

这些客户端使用 Python 3.10+ 标准库，用于应用接入；RunDesk 服务及可运行包不依赖它们。

| File / 文件 | Purpose / 用途 |
| --- | --- |
| `application_client.py` | RunDesk application API client / 应用 API 客户端 |
| `application_quickstart.py` | Minimal application task flow / 最小应用任务流程 |
| `uhp_client.py` | UHP upload, submit, poll, continue and artifact commands / UHP 文件、任务与制品操作 |
| `activevlm_client.py` | ActiveVLM application integration / ActiveVLM 应用接入 |

Keep `application_quickstart.py` and `application_client.py` together when copying
them. Configure the server URL and credentials described in each file; do not
write tokens into source files.

复制最小接入程序时，请保留同目录的 `application_client.py`。服务地址和凭据通过各文件说明的环境变量传入。

```bash
python3 clients/python/uhp_client.py --help
python3 clients/python/application_quickstart.py --help
```

See the [application API](../../docs/API-V1.md), [application connection
guide](../../docs/APPLICATION-CONNECT.md), and [UHP guide](../../docs/UHP.md).
