# 通知脚本（替代 ealert）

本部署不接入 ealert。流程通知（审批待办、抄送、进度、驳回、撤销、自动化结果）由 eflow 渲染好内容后，
提交给 ecmdb-task 中的一个**通知执行单元（Runner）**，由脚本负责真正投递到飞书 / 企微 / 邮件等渠道。

## 配置

```yaml
notify:
  runner_id: 12              # ecmdb-task 中通知脚本对应的执行单元 ID；不配置则退回飞书/企微应用直连发送
  max_execution_seconds: 60  # 单次通知脚本最长执行时间，<=0 使用 ecmdb-task 默认值
  timeout: 5s                # 提交执行的超时时间
```

- 每条通知（每个接收人）提交一次执行，eflow 只等待"提交成功"，不等待脚本执行结果。
- 提交失败只记录日志，不影响流程推进。
- 幂等请求标识以 `notify:` 开头，ecmdb-task 不会把这类执行的结果回传给自动化任务。
- 脚本模式不支持"原地更新已发送的卡片"（如审批后卡片置灰），这类更新会被跳过。

## 脚本参数

与自动化节点一致，参数以 JSON 字符串放在 `args` 中：

```json
{
  "kind": "eflow.notification",
  "channel": "LARK_CARD",            // LARK_CARD | LARK_TEXT | WECHAT（流程配置的通知渠道）
  "receiver": "ou_xxx",              // 接收人：飞书 user_id / 企微 userid / 群聊 chat_id
  "receiver_type": "user_id",        // user_id | chat_id
  "workflow_id": 9,
  "notify_type": "approval",         // approval | carbon-copy | chat | progress | progress-image-result | revoke | reject
  "template": {
    "name": "approval",
    "title": "【待审批】服务器申请",
    "fields": [{"is_short": true, "tag": "lark_md", "content": "**申请人**\n张三"}],
    "values": [{"key": "task_id", "value": "123"}],
    "input_fields": [],
    "hide_form": false,
    "remark": "",
    "image_key": "",
    "text": ""                       // LARK_TEXT 渠道的纯文本内容
  }
}
```

## 最小示例（Python）

```python
import json, os, sys, urllib.request

args = json.loads(os.environ.get("args") or sys.argv[1])
if args.get("kind") != "eflow.notification":
    sys.exit(0)

tpl = args["template"]
lines = [tpl["title"]] + [f["content"] for f in tpl.get("fields", []) if f.get("content")]
text = tpl.get("text") or "\n".join(lines)

# 以飞书机器人 webhook 为例：按需替换成企微、邮件或公司内部消息网关
payload = {"msg_type": "text", "content": {"text": f"@{args['receiver']}\n{text}"}}
req = urllib.request.Request(os.environ["FEISHU_WEBHOOK"], data=json.dumps(payload).encode(),
                             headers={"Content-Type": "application/json"})
print(urllib.request.urlopen(req, timeout=10).read().decode())
```

webhook 地址、应用密钥等敏感信息放在执行单元的**加密变量**中，由 ecmdb-task 在执行时注入，不要写进脚本。
