# 应用 API 凭据

管理员进入「应用 → 具体应用 → API 凭据」，创建凭据，选择项目、权限、可选有效期。完整值只显示一次；数据库只存 SHA-256 哈希。将凭据放在 news2douyin / video app 后端环境变量中，不放在浏览器前端。

一个凭据固定对应一个应用及其专用配置。所有请求必须同时满足应用、配置、允许项目三项归属。请求未传 instanceId / source 时自动绑定；明确传入其他归属会拒绝。应用只能看自己的会话、任务、规则；项目列表只返回 id/name。管理员仍用原 Token/Cookie。默认本机无 Token 模式保留旧行为：本机访问即管理访问，不要对不可信用户开放。

| 权限 | 能力 |
|---|---|
| read（必需） | 本应用会话、任务、消息、轨迹、审批状态、导出、幂等回执 |
| run | 创建/继续/停止/取消任务，修改或删除自己的会话 |
| schedules（还需 run） | 预览和管理自己的定时规则 |
| files | 允许项目的 uploads / outputs 上传、文件读取和文件列表 |
| approvals（还需 run） | 回应本应用原生执行审批 |

模型、skills、MCP、账户、全局并发、应用注册和凭据管理只允许管理员。应用凭据不支持创建轨迹分析会话，不支持读配置或改变评分。未知路由和方法默认拒绝，/api 与 /api/v1 同样校验。

```python
import os
from application_client import RunDesk
client = RunDesk(token=os.environ['RUNDESK_APP_TOKEN'])
identity = client.whoami()
# 先将以下 operation key 和请求体保存到业务数据库，再提交。
task = client.create_task({
    'workspaceId': '管理员授权的项目编号',
    'title': '新闻背景研究',
    'input': {'text': '围绕这条新闻核对原始来源、背景与争议。'}
}, key='news-research-operation-0001')
print(task['id'], task['sessionId'])
```

超时后用同一个凭据与幂等键查询 receipt，再重试原请求。幂等空间按 credential.id 隔离；两个凭据即使同属一个应用也有不同空间。轮换凭据后不要盲目用旧业务 key 再提交：应先按 task/session ID 查现有任务。source.taskId 只是标签。

GET/POST /applications/{appId}/keys 管理元数据/创建；DELETE /applications/{appId}/keys/{keyId} 撤销；这些为管理员接口。创建响应 {credential, token} 不缓存幂等回执，响应丢失则在列表中定位新凭据、撤销后重建，不能找回原值。

撤销或过期阻止后续请求；SSE 在下一次轮询核对后断开（约 300ms，正在写出的帧可能先到达）。从 0.19.2 起，新应用任务（包括 A2A）与定时规则记录 submittingKeyId。排队任务在启动前复核该凭据，撤销／过期／失去授权后以 failed 留下原因，不启动 Agent；定时规则到期复核后停用。已运行任务仍需显式停止。旧版无绑定记录的任务、管理员及内部提交保留原行为。更换凭据后，用新凭据保存定时规则可重新授权，但旧排队任务不会自动换绑。显式 Authorization 优先于管理员 Cookie，避免权限意外提升。

这不是完整多用户 RBAC，也不是操作系统隔离。Codex 原生执行仍依据该应用配置的 sandbox 与审批策略；共享文件系统、项目、服务环境和密钥环需管理员管理。files 权限还按应用／实例／项目校验上传归属，输出按所属会话校验；同一应用项目内按共享授权协作。read 返回的轨迹可能包含工具输出和路径。不同信任主体应使用独立系统账户、数据目录与项目，容器不是必需。
