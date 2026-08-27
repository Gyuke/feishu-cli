# 妙搭（Miaoda）应用（HTML 秒搭一键部署）

把一份 HTML（单文件或整目录）按官方三段协议发布成妙搭应用，拿到 `release_id`。

> **feishu-cli**：如尚未安装，请前往 [riba2534/feishu-cli](https://github.com/riba2534/feishu-cli) 获取安装方式。

## 前置条件

- **认证**：所有 `apps` 命令都需要 **User Access Token**
- **scope**：`spark:app:write`（create / update / html-publish / access-scope-set）、`spark:app:read`（html-publish 校验 app_type、access-scope-get）
- **登录**：`auth login` 是增量授权，补授 spark scope 不会丢掉已有授权：

```bash
feishu-cli auth check --scope "spark:app:write"          # 先预检当前是否已有
feishu-cli auth login --scope "spark:app:read spark:app:write"
```

`apps list` 是故意隐藏的内部命令，不作为 Agent 枚举入口；更新或发布已有应用时让用户提供 app_id。

## 典型流程（三步部署）

```bash
# 1. 创建一个 HTML 应用，拿 app_id（CLI 已剥掉飞书响应的 data 外层，jq 路径为 .app.app_id）
feishu-cli apps create --name "我的页面" --app-type HTML
# 2. 把 HTML 目录/文件按三段协议打包发布，返回 release_id（jq 路径为 .release_id）
feishu-cli apps html-publish --app-id app_xxx --path ./dist
# 3. 设访问范围（默认创建后通常仅自己可见）
feishu-cli apps access-scope-set --app-id app_xxx --scope tenant
```

## 命令速查

### 1. 创建应用 `apps create`

```bash
feishu-cli apps create --name "我的页面" --app-type HTML
feishu-cli apps create --name "Dashboard" --app-type HTML --description "数据看板" --icon-url https://...
feishu-cli apps create --name x --app-type HTML --dry-run     # 只看将要发的请求
```

- `--app-type` 当前只支持 `HTML`
- 返回里取 `.app.app_id` 给后续命令用（CLI 已剥掉飞书响应的 `data` 外层，故不是 `.data.app.app_id`）

### 2. 发布 HTML `apps html-publish`（一键部署）

```bash
feishu-cli apps html-publish --app-id app_xxx --path ./dist          # 目录形态
feishu-cli apps html-publish --app-id app_xxx --path ./index.html    # 单文件形态
feishu-cli apps html-publish --app-id app_xxx --path ./dist --dry-run        # 看打包清单 + 凭证扫描
feishu-cli apps html-publish --app-id app_xxx --path ./dist --allow-sensitive  # 放行凭证文件
```

- **必须有 index.html**：目录形态根目录下要有 `index.html`；单文件形态文件名必须就是 `index.html`（妙搭以它作为应用入口）
- **app_type**：仅 `html` / `modern_html` 可走本命令；实跑会先 GET 应用校验，其它类型非零退出并给出恢复建议
- **三段协议**（退役单 POST `/upload_and_release_html_code`）：GET `pre_release` 解析 `upload_url`/`tos_path` → 对预签名 URL PUT tar.gz（**不携带飞书 Authorization**）→ POST `/apps/{id}/releases` body `{"tos_path":...}` 返回 `release_id`
- **打包方式**：`--path` 整个打包成单个 in-memory tar.gz；未压缩 ≤ 200MB、打包后 tar.gz ≤ 20MB、单个 `.html` 文件 ≤ 10MB（妙搭服务端硬约束，超限客户端提前拦截并点名文件，`--dry-run` 回填 `oversize_html`）
- **凭证文件防呆**：默认拦截 `.env` / `.env.*` / `.npmrc` / `.netrc` / `.git-credentials` / `.aws/credentials` / `.docker/config.json` / `.kube/config`，命中即非零退出（`--dry-run` 也拦）；确实要发布加 `--allow-sensitive`
- **`--dry-run`**：只展示三段计划 + 打包清单，不获取 token、不访问网络、不上传
- 返回里取 `.release_id`（CLI 只白名单提取这一个字段；不再返回 `.url`）

### 3. 修改应用 `apps update`

```bash
feishu-cli apps update --app-id app_xxx --name "新名字"
feishu-cli apps update --app-id app_xxx --description "更新后的描述"   # --name / --description 至少一个
```

### 4. 访问范围 `apps access-scope-get` / `apps access-scope-set`

```bash
feishu-cli apps access-scope-get --app-id app_xxx

feishu-cli apps access-scope-set --app-id app_xxx --scope tenant        # 组织内可见
feishu-cli apps access-scope-set --app-id app_xxx --scope public --require-login=true   # 互联网公开（require-login 必填）
feishu-cli apps access-scope-set --app-id app_xxx --scope specific \
  --targets '[{"type":"user","id":"ou_xxx"},{"type":"department","id":"od_xxx"},{"type":"chat","id":"oc_xxx"}]'
feishu-cli apps access-scope-set --app-id app_xxx --scope specific \
  --targets '[{"type":"user","id":"ou_xxx"}]' --apply-enabled --approver ou_appr   # 开放申请 + 审批人
```

- `--scope` 三选一：`specific`（部分人员，映射后端 `Range`）/ `public`（互联网公开，映射 `All`）/ `tenant`（组织内，映射 `Tenant`）
- `specific`：必须配 `--targets`（统一格式，发请求时自动拆成后端的 users/departments/chats）
- `public`：必须显式给 `--require-login`（true/false，不能依赖默认）
- `tenant`：不接受其它 flag

## app_id 怎么来

- 自己刚 `apps create` 的：从返回的 `.app.app_id` 取（CLI 已剥掉 `data` 外层）
- 别人/已有的应用：让用户给妙搭应用链接，从 `https://miaoda.feishu.cn/app/app_xxx` 里 `/app/` 后面那段提取，或直接给 `app_xxx` 字符串

## 输出与排错

- 所有命令支持 `--format json|pretty|table|ndjson|csv` + `--jq`，写命令支持 `--dry-run`（`--dry-run` 同样尊重 `--format/--jq`）
- 输出已剥掉飞书响应的 `data` 外层：`apps create` 直接是 `{"app":{"app_id":...}}`（jq 用 `.app.app_id`），`apps html-publish` 直接是 `{"release_id":...}`（jq 用 `.release_id`）
- 业务错误 `code=400002577` / `90002` = 应用不存在或无权访问（核对 app_id）；`code=400000059` = app_type 不支持 html-publish；`code=90001` = 服务端构建失败（用 `--dry-run` 检查打包文件清单）
- TOS PUT 4xx/5xx 会中止、不调用 release-create；5xx 可重试同一条命令换新预签名 URL。不要把飞书 Authorization 带到 TOS。
- scope 不足报错时：`feishu-cli auth check --scope "spark:app:read spark:app:write"` 预检，再按上面「前置条件」并入完整 scope 重新登录
