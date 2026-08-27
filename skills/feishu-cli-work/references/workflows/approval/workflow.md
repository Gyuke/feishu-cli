# 飞书审批技能（查询 + 写入）

通过 feishu-cli 完成审批全生命周期：查询审批定义 / 审批实例 / 待办任务，发起 / 撤回 / 抄送审批实例，通过 / 拒绝 / 转交审批任务。

> **feishu-cli**：如尚未安装，请前往 [riba2534/feishu-cli](https://github.com/riba2534/feishu-cli) 获取安装方式。

## 核心概念

飞书审批由四级对象组成，命令按对象分组：

| 对象 | 说明 | 唯一 ID | CLI 子命令 |
|------|------|---------|-----------|
| **definition** | 审批定义（审批流模板，行政/财务后台配置） | `approval_code` | `approval get` |
| **instance** | 审批实例（一次具体的发起，绑定一个 definition + 一份 form） | `instance_code` | `approval instance {get,initiated,create,cancel,cc}` |
| **task** | 审批任务（实例分发到每个审批节点上的待办） | `task_id` | `approval task {query,approve,reject,transfer}` |
| **cc** | 抄送（把实例送到其他用户阅知，非审批节点） | — | `approval instance cc` |

**生命周期**：`approval get` → 提交 form 触发 `instance create` → 节点用户 `task approve/reject` → 发起人可中途 `instance cancel` 或 `instance cc` 抄送他人。查「我发起的」优先 `approval instance initiated`。

## 身份说明（Token 策略）

**全部当前审批 API 都使用 User Token**，必须先 `feishu-cli auth login`。发起人 / 审批人身份取当前登录用户，不再传 `--user-id`，也不再回退 Tenant Token。

### 所需 scope

| 命令 | scope | Token 类型 |
|------|-------|-----------|
| `approval get` | `approval:approval:read` | **User Token 必需** |
| `approval instance get` | `approval:instance:read` | **User Token 必需** |
| `approval instance initiated` | `approval:instance:read` | **User Token 必需** |
| `approval task query` | `approval:task:read` | **User Token 必需** |
| `approval instance create` | `approval:instance:write` | **User Token 必需** |
| `approval instance {cancel,cc}` | `approval:instance:write` | **User Token 必需** |
| `approval task {approve,reject,transfer}` | `approval:task:write` | **User Token 必需** |

```bash
feishu-cli auth check --scope "approval:approval:read approval:instance:read approval:instance:write approval:task:read approval:task:write"
feishu-cli auth login --scope "approval:approval:read approval:instance:read approval:instance:write approval:task:read approval:task:write offline_access"
```

### 当前 path

| 命令 | HTTP | path |
|------|------|------|
| `approval get` | GET | `/open-apis/approval/v4/approvals/{approval_code}/detail` |
| `approval instance get` | GET | `/open-apis/approval/v4/instances/detail` |
| `approval instance initiated` | GET | `/open-apis/approval/v4/instances/initiated` |
| `approval instance create` | POST | `/open-apis/approval/v4/instances/initiate` |
| `approval instance cancel` | POST | `/open-apis/approval/v4/instances/recall` |
| `approval instance cc` | POST | `/open-apis/approval/v4/instances/add_cc` |
| `approval task query` | GET | `/open-apis/approval/v4/tasks` |
| `approval task approve` | POST | `/open-apis/approval/v4/tasks/pass` |
| `approval task reject` | POST | `/open-apis/approval/v4/tasks/refuse` |
| `approval task transfer` | POST | `/open-apis/approval/v4/tasks/forward` |

`task query` **不传** `user_id` query。HTTP 200 但业务 `code != 0` 时，包括 `--output raw-json` 也会非零退出。

## 命令速查

### 读

```bash
# 查审批定义（拿表单结构 / 节点列表，发起前必看）
feishu-cli approval get <approval_code>
feishu-cli approval get <approval_code> --output raw-json

# 查我的审批任务（topic 仅接受 todo / done / cc-unread / cc-read）
feishu-cli approval task query --topic todo
feishu-cli approval task query --topic done
feishu-cli approval task query --topic cc-unread
feishu-cli approval task query --topic cc-read
# 注意：topic=started 已被官方 tasks 接口下线（服务端回 99992402
# "topic is optional, options: [1,2,17,18]"），查「我发起的」用下面的专用入口

# 查我发起的审批实例（专用入口）
feishu-cli approval instance initiated
feishu-cli approval instance initiated --definition-code <code> --output json

# 查单个审批实例详情
feishu-cli approval instance get --instance-code <ic>
feishu-cli approval instance get --instance-code <ic> --output raw-json
```

### 写

```bash
# 发起审批实例（身份取当前 User Token；form 可选，传入时必须是 JSON 数组）
feishu-cli approval instance create \
  --approval-code <code> \
  --form-file form.json \
  --node-approver-file node-approvers.json \
  --node-cc-file node-cc.json
#   或：--form '[{"id":"widget_1","type":"input","value":"内容"}]'

# 撤回已发起的审批实例（只有发起人能撤）
feishu-cli approval instance cancel --instance-code <ic>

# 抄送实例给其他用户（逗号分隔，自动去重保留首次顺序）
feishu-cli approval instance cc \
  --instance-code <ic> \
  --cc-user-ids ou_a,ou_b \
  --comment "请知悉"

# 通过 / 拒绝审批任务
feishu-cli approval task approve \
  --instance-code <ic> --task-id <task> \
  --comment "同意"

feishu-cli approval task reject \
  --instance-code <ic> --task-id <task> \
  --comment "金额超预算"

# 转交审批任务
feishu-cli approval task transfer \
  --instance-code <ic> \
  --task-id <task> \
  --transfer-user-id ou_target \
  --comment "请代审"
```

## 关键 flag

### `--approval-code`（`instance create` / `approval get` 必填）

审批定义 code，可从 `approval get` 输出或飞书后台审批管理页 URL 拿到。CLI 会先用 `isValidToken` 校验格式。`instance cancel/cc` 与 `task approve/reject/transfer` 只需 `--instance-code`（task 系列额外要 `--task-id`）。

### `--user-id-type`

`instance create` **不再接收** `--user-id`：发起人就是当前 User Token。`--user-id-type` 只用于说明抄送人 / 被转交人 / 列表返回 ID 的类型（`open_id` / `user_id` / `union_id`），默认 `open_id`。`task transfer` 的 `--transfer-user-id` 表示被转交人。

### `--form` 与 `--form-file`

`instance create` 的 form 可选；传入时 `--form` / `--form-file` 二选一。`task approve` 支持 `--form`；`task reject` 不支持 form。

**form 必须是 JSON 数组**，否则 CLI 在客户端先报 "表单数据必须是 JSON 数组，解析失败"：

```json
[
  {"id": "widget_1", "type": "input",    "value": "差旅报销 1500"},
  {"id": "widget_2", "type": "number",   "value": 1500},
  {"id": "widget_3", "type": "textarea", "value": "上海出差 3 天"}
]
```

widget 的 `id` / `type` 从 `approval get --output raw-json` 的 `data.form` 字段读，**不要手编**。各控件 `value` 结构见 [references/form-control-values.md](references/form-control-values.md)。

### `--cc-user-ids`（`instance cc` 必填）

逗号分隔列表，例如 `ou_a,ou_b,ou_a`。CLI 自动 trim + 去重，保留首次出现顺序。

### `--comment`（可选，approve/reject/transfer/cc 共用）

审批意见 / 抄送备注。`task reject` 建议填写拒绝原因。

### `--node-approver` / `--node-cc`（仅 `instance create`）

节点审批人 / 抄送人，官方 body 字段是 `node_approver_list` / `node_cc_list`，每项 `{ "key": "<node_id 或 custom_node_id>", "value": ["ou_xxx"] }`。CLI 仍接受旧的 `node_id` 键并映射为 `key`。支持对应 `--*-file`。

### `--uuid`（仅 `instance create`）

可选幂等键。

### `task query` / `instance initiated` 列表字段

- `count`：整数，通常只在第一页返回；≥100 时常返回 99
- 任务：`task_id`、`instance_code`、`instance_status`、`initiator`、`initiator_name`、`summaries`、`support_api_operate`
- 已发起实例：`instance_code`、`definition_code`、`instance_status`、`initiator`、`initiator_name`、`summaries`

### `--user-access-token`

覆盖 token 解析链最顶端。多数情况无需指定，自动从 `~/.feishu-cli/token.json` 读已登录态。

## 完整用例

### 例 1：从零到通过一条报销审批

```bash
# 1. 登录并预检用户态 scope（定义读取 + 实例/任务读写）
feishu-cli auth login --scope "approval:approval:read approval:task:read approval:task:write approval:instance:read approval:instance:write offline_access"

# 2. 查审批定义拿 widget 结构
feishu-cli approval get 7AB12C... --output raw-json

# 3. 准备 form.json
cat > /tmp/form.json <<'EOF'
[
  {"id": "widget_1", "type": "input",  "value": "差旅报销"},
  {"id": "widget_2", "type": "number", "value": 1500}
]
EOF

# 4. 以当前登录用户身份发起实例
feishu-cli approval instance create \
  --approval-code 7AB12C... \
  --form-file /tmp/form.json
# → 输出：审批实例已创建  instance_code / instance_link

# 5. 节点审批人通过任务（先在审批人账号上 auth login）
feishu-cli approval task query --topic todo
feishu-cli approval task approve \
  --instance-code 8XY99Z... \
  --task-id 99TASK... \
  --comment "同意"

# 可选：查看实例详情或「我发起的」列表
feishu-cli approval instance get --instance-code 8XY99Z... --output json
feishu-cli approval instance initiated --output json
```

### 例 2：发起后撤回 + 抄送

```bash
feishu-cli approval instance cancel --instance-code <ic>

feishu-cli approval instance cc \
  --instance-code <ic> \
  --cc-user-ids ou_a,ou_b,ou_a \
  --comment "供参考"

feishu-cli approval task transfer \
  --instance-code <ic> --task-id <task> \
  --transfer-user-id ou_target \
  --comment "请代审"
```

## 踩坑

| 问题 | 原因 | 解决 |
|------|------|------|
| `表单数据必须是 JSON 数组，解析失败` | `--form` 传了 `{...}` 对象 | 包成数组 `[{...}]` |
| `--cc-user-ids` 重复 ID 抄送多次 | 不会，CLI 已去重 | 如需多次提示，多次执行 `instance cc` |
| `task approve` 返回 forbidden / `code=1395001` | 当前用户不是审批人 / 任务已被处理 | 先 `task query --topic todo`，看 `support_api_operate` |
| `instance cancel` 失败 | 当前用户不是发起人 / 实例已结束 | 只有发起人能撤 |
| `widget id` 找不到 | 手编 ID | 先 `approval get --output raw-json` 看 `data.form` |
| HTTP 200 但 CLI 非零退出 | 飞书业务 `code != 0`，raw-json 也会失败 | 读错误里的 `code=` / `msg=` |
| User Token 缺失 | 全部审批命令都要用户身份 | 先 `feishu-cli auth login`，或显式 `--user-access-token` |

## 输出格式

写命令默认输出单行成功摘要；`instance create` 还会打印 `instance_link`（若返回）。

读命令 `approval get` / `approval instance get` / `approval instance initiated` / `task query` 支持：

- 不传 `--output`：人类可读文本摘要
- `--output json`：CLI 归一化 JSON
- `--output raw-json`：飞书 API 原始成功响应；业务 `code != 0` 时不会把失败 envelope 当成功输出

`approval instance create` 和 `approval task transfer` 支持 `--output json`。

## 不在本技能范围

| 需求 | 走哪里 |
|------|--------|
| 审批流可视化设计 | 飞书后台「审批管理」Web UI |
| 搜可发起定义（`approvals/search_launchable`） | 后续覆盖 lane，当前用已知 `approval_code` + `approval get` |
| 退回 / 加签 / 催办（`tasks/rollback` / `add_sign` / `remind`） | 后续覆盖 lane |
| 审批回调订阅 | `feishu-cli event consume approval.instance.status_changed_v4` 等 |
| 审批结果二次通知到群 | `feishu-cli-messaging` |
| 给审批文档评论 / 加权限 | `feishu-cli-storage` |

## 相关 skill

- `/feishu-cli-platform` — OAuth 登录、scope 预检、token 状态
- `/feishu-cli-work` — 综合查询入口
- `/feishu-cli-messaging` — 审批结果二次通知到群 / 个人
