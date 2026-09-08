# 知识库增量同步使用指南（`wiki sync pull / subscribe / watch / reconcile / status`）

本文是 `feishu-cli wiki sync` 命令组的**使用文档**：从「把云端知识库拉取到本地（指定保存目录）」、
「建立事件订阅」到「常驻监听文档改动并定向重导」与「定时全量对账」的完整流程，以及每步生成的
元数据和文档存放在哪里、被哪些命令消费。文中的 URL、token 均为占位符，实际使用请替换，并**不要**
把企业前缀域名或真实凭证写入仓库。

> 对应旧命令：`wiki export-tree <url> -o <dir>`（一次性全量拉取）已被 `wiki sync pull` 取代。
> `temp.md` 里的 `./bin/feishu-cli wiki export-tree '...' -o ./doc_sop_test`，等价于
> 在 `wiki-sync.yaml` 里配 `wiki_url` + `local_dir: ./doc_sop_test` 后跑 `wiki sync pull`。
>
> 两种「变更→重导」路径：`watch`（实时，常驻长连接）与 `reconcile`（周期，cron 每日跑一次）。
> `reconcile` 用节点 `obj_edit_time` 与上次基线比对，只重导改过的文档，无需常驻进程，两者可共存。

---

## 1. 前置条件

1. **配置一个 App 并授予权限**（一次性）：`feishu-cli config create-app --save`，然后在
   开放平台粘贴 README 的权限 JSON 一次开通。
2. **用户授权**（订阅与读取文档都以文档**拥有者**的 User Token 进行）：
   ```bash
   feishu-cli auth login --domain <名称> --recommend
   # 或显式 scope：
   feishu-cli auth login --scope "docx:document docx:document:readonly docs:event.document_edited:read drive:drive"
   ```
3. 在开放平台为应用开启「事件订阅 - 长连接接收事件」，并勾选/发布 `drive.file.edit_v1`、
   `drive.file.deleted_v1` 事件。
4. 确认应用对目标知识空间有读取权限。

> 注意：`wiki sync subscribe` 与 `wiki sync watch` 都**需要** User Access Token（缺 `--user-access-token`
> 时优先取登录态 / `~/.feishu-cli/token.json`，缺失则报错，不会回退到 App Token）。

---

## 2. 配置 `wiki-sync.yaml`

默认读取 `~/.feishu-cli/wiki-sync.yaml`，可用 `--config` 覆盖。一个任务（`queries` 一项） =
「一个 wiki_url **或** space_id → 一个本地保存目录」。

```yaml
options:
  debounce: 10s            # 同一文档两次定向重导的最小间隔（watch 用）
  download_images: true    # 拉取时下载图片到 assets_dir
  clean: false             # 是否严格镜像：云端删了本地是否也删（reconcile 用；false=保留+索引标 gone）
  continue_on_error: true  # 单个节点失败是否继续
  include_types: [docx, sheet]

queries:
  # ① 单节点任务：拉取某节点子树（根节点 URL）
  - name: "叉车取货文档"
    wiki_url: "https://example.feishu.cn/wiki/WIKI_NODE_TOKEN"   # 根节点 URL（二选一，见下）
    local_dir: "./doc_sop_test"                                  # 保存目录（必填，对应旧 -o <dir>）
    # assets_dir: "./doc_sop_test/assets"   # 省略则默认 <local_dir>/assets
    # conflict: "overwrite"                 # overwrite | skip | fail
    # debounce: "10s"                        # 覆盖 options 层

  # ② space 任务：拉取整个知识库（枚举 space 全部顶层节点并镜像整库）
  - name: "FAQ 帮助中心"
    space_id: "7349730005238317084"                              # 知识库 space_id（二选一，见下）
    local_dir: "./docs/faq"
    # wiki_url: "https://seer-group.feishu.cn/wiki/<任一篇>"     # 可选：用于生成节点 URL 的 host 与标识；省略回退 feishu.cn
```

关键字段：

| 字段 | 必填 | 说明 |
|------|------|------|
| `wiki_url` | 二选一 | 单节点任务的根节点 URL。`task_hash = hex(sha256(wiki_url))` 由此派生（任务身份，不随目录移动变） |
| `space_id` | 二选一 | space 任务：整个知识库的 space_id。`task_hash = hex(sha256("space:"+space_id))`；`wiki_url` 此时可选（仅用于节点 URL 的 host 与标识） |
| `local_dir` | 是 | 本地保存目录（对应旧命令的 `-o <dir>`） |
| `assets_dir` | 否 | 图片资源目录，默认 `<local_dir>/assets` |
| `include_types` | 否 | 导出类型，默认 `docx, sheet`（watch 只对 `docx` 订阅/重导） |
| `debounce` | 否 | 事件聚合窗口，默认 `10s` |

> `wiki_url` 与 `space_id` 二选一：填 `wiki_url` = 单节点任务；填 `space_id` = 整库任务。
> space 任务的目录布局为**按结构嵌套**：顶层叶子 → `<local_dir>/<标题>.md`；顶层有子节点的 →
> `<local_dir>/<标题>/<标题>.md`（其子文档挂在该子目录下）。`reconcile` 对 space 任务同样生效。

> `local_dir` / `assets_dir` 支持 `~/`（如 `~/Documents/seer_docs/...`），会在解析时展开为
> 用户主目录，避免被当作字面量目录名。也可以直接用绝对路径（`/home/<user>/...`）或相对路径。

---

## 3. 完整命令（拉取 + 订阅 + 变更→重导）

```bash
# ① 全量拉取到本地（生成 Markdown + 索引 + manifest）
feishu-cli wiki sync pull --config ~/.feishu-cli/wiki-sync.yaml

# ② 建立订阅（对索引里的 docx 批量订阅云端事件，回写索引 subscribe_status）
feishu-cli wiki sync subscribe --config ~/.feishu-cli/wiki-sync.yaml

# ③ 变更→重导，二选一：
#   A) 实时：常驻监听文档改动并定向重导（Ctrl-C 退出）
feishu-cli wiki sync watch --config ~/.feishu-cli/wiki-sync.yaml
#   B) 周期：cron 每日跑一次，只重导自上次对账后改过的文档（无需常驻进程）
feishu-cli wiki sync reconcile --config ~/.feishu-cli/wiki-sync.yaml

# ④ 只读巡检：看一眼每个任务的索引/订阅/对账基线/事件记录（不调 API 不写文件，适合 cron 前置检查）
feishu-cli wiki sync status --config ~/.feishu-cli/wiki-sync.yaml
```

一次性脚本（把 ① ② 逐条执行，③ 任选其一；`watch` 常驻、`reconcile` 可置入 cron）：
```bash
feishu-cli wiki sync pull      --config ~/.feishu-cli/wiki-sync.yaml
feishu-cli wiki sync subscribe --config ~/.feishu-cli/wiki-sync.yaml
feishu-cli wiki sync watch     --config ~/.feishu-cli/wiki-sync.yaml   # 或 reconcile
feishu-cli wiki sync status    --config ~/.feishu-cli/wiki-sync.yaml   # 巡检
```

### 3.1 `reconcile`（定时对账）用法

```bash
# 增量：只重导自上次对账基线后改动/新增的文档；无基线则第一次为全量
feishu-cli wiki sync reconcile --config ~/.feishu-cli/wiki-sync.yaml

# 按日历边界：只重导今天改过的
feishu-cli wiki sync reconcile --config scripts/wiki-sync.yaml --since today
# 显式基线：--since <unix秒> | <RFC3339> | now（全量）
```
每次成功对账后会把**该任务**的下次基线写回（`~/.feishu-cli/state/wiki-sync/tasks/<task_hash>/last-reconcile.json`）。
基线目录按**任务身份**（`local_dir + task_identity`）派生，与配置文件哈希**解耦**：改 `clean`、加注释、改其它字段都**不会**重置基线、触发全量；只有真正换任务（`wiki_url` / `space_id` 或 `local_dir` 变）才需要重来。`task_identity` = 单节点任务的 `wiki_url`，或 space 任务的 `"space:"+space_id`；`<task_hash> = hex(sha256(local_dir + "\x00" + hex(sha256(task_identity))))`。

> ⚠ **首次运行会全量重导，属正常引导**：没有基线时 `cutoff=0`，所有节点都被视作"变更"全量重导。
> 这不是异常，也顺便把空 `obj_edit_time` 索引补全。若想跳过首次全量，可先
> `--since <近期时间戳>` 打一个锚点再继续增量。从第二次起就是纯增量。

对账除了按 `obj_edit_time > 基线` 识别**内容变更**，还会处理**结构变化**：
- **改名 / 移动**：标题（`title`）或父节点（`parent_node_token`）与索引不一致 → 视为变更，重导到新路径并校正索引 `local_path`；旧 `.md` 仅在 `clean: true` 时删除（导成功后，避免中途失败丢数据）。
- **删除 / 移出 / 回收站**：节点从这次枚举中消失 → 记一条 `gone` 并在索引里标 `sync_status: "gone"`；`clean: true` 时进一步把该文件从磁盘 + 清单剔除，`clean: false` 时保留磁盘文件仅供查看。
- **孤儿清理（clean 兜底）**：每次对账都会以「最终索引 + 磁盘资产」为权威重建本任务的活集（每个索引条目的 `.md` + 各 `.md` 的图片资产子目录），与旧清单做差——凡是移动 / 重命名 / 删除后**不再被任何索引条目引用**的旧 `.md`、旧 assets（例如文档从 `A/名字.md` 挪到根目录后，`A/名字.md` 那一条就成孤儿）都会被识别：`clean: true` 物理删除并停止追踪；`clean: false` 保留磁盘文件仅供查看，仅从 manifest 移除。旧清单里记录过、但当前索引已不存在的路径由此得到清理，不再永久遗留。

常见参数（加在对应命令后）：

| 命令 | 参数 | 作用 |
|------|------|------|
| pull | `--dry-run` | 只打印将导出的内容，不调 API 不写文件 |
| subscribe | `--verify` | 先回查服务端订阅真值再补订（纠正本地漂移；每个 docx 一次 GET） |
| subscribe | `--dry-run` | 只列出将订阅的 docx |
| watch | `--no-precheck` | 跳过启动前的「订阅回查+补订」预检（默认开启） |
| watch | `--timeout 30s` | 跑 30s 后自动退出（定时巡检 / 测试用） |
| watch | `--max-events 1` | 处理 1 次重导后自动退出（自检用） |
| reconcile | `--since today` | 以当日 00:00 为界，只重导今天改过的 |
| reconcile | `--since <unix秒/\|RFC3339>` | 以指定时间为界；`now`=全量 |
| status | `--format json/pretty/table` `--jq '...'` `-o <file>` | 只读聚合输出格式 / jq 过滤 / 写文件 |
| 各命令（除 status） | `--user-access-token <T>` | 显式指定 User Token（否则取登录态） |

---

## 4. 每个命令的产物：位置 + 内容 + 谁消费

### 4.1 产物总览（按 `local_dir` 下）

```text
<local_dir>/
  <知识库层级>.md                      # 镜像下来的文档内容（每篇一个 .md）
  <子目录>/<文档>.md
  assets/                              # 下载的图片（download_images=true 时）
    <文档名>/image_N.png
  .feishu-cli/
    wiki-index.json                    # 合并索引（全部任务，一项 = 一个本地 Markdown）
    manifests/<task_hash>.json         # 单任务文件清单（该任务写过的所有文件）
    changes/<YYYY-MM-DD>.jsonl         # watch 每次重导/失败/删除的逐行记录
```

全局（跨任务）状态在 `~/.feishu-cli/state/wiki-sync/<config_hash>/subscriptions.json`，
其中 `<config_hash> = hex(sha256(wiki-sync.yaml 文件字节))`。

### 4.2 `wiki sync pull` 生成什么

| 产物 | 位置 | 内容 | 被谁消费 |
|------|------|------|----------|
| 文档正文 | `<local_dir>/<...>.md` | 云端文档镜像（Markdown） | 人 / RAG / Git |
| 图片资源 | `<local_dir>/assets/<...>/*.png` | 拉取时下载 | 文档正文引用、Git |
| 合并索引 | `<local_dir>/.feishu-cli/wiki-index.json` | 一次拉取合并所有任务；一项含 `task_id / space_id / node_token / obj_token / obj_type / title / parent_node_token / has_child / node_type / obj_edit_time / wiki_url / local_path / subscribe_status / last_subscribed_at` | `subscribe`（过滤 docx）、`watch`（按 `obj_token` 反查 `local_path`+`node_token` 重导）、RAG（URL→本地路径） |
| 任务 manifest | `<local_dir>/.feishu-cli/manifests/<task_hash>.json` | `task_id / wiki_url / local_dir / files`（本任务写过的全部 `.md`+`assets` 相对路径，去重排序） | `clean`（只删本任务文件）、`reconcile`（结构变化检测）、事件调度 |

`pull` 逐任务执行：遍历 → 导出 → 下载图片 → 写 `.md` → `SaveManifest` → `mergeIndexForTask`（并入合并索引）。

### 4.3 `wiki sync subscribe` 生成什么

| 产物 | 位置 | 内容 | 被谁消费 |
|------|------|------|----------|
| 回写的索引 | `<local_dir>/.feishu-cli/wiki-index.json` | 把已订阅的 docx 条目 `subscribe_status` 置为 `subscribed`、记 `last_subscribed_at` | `watch`（启动预检对账）、`status`（Phase 4）、人工对账 |
| 全局汇总 | `~/.feishu-cli/state/wiki-sync/<config_hash>/subscriptions.json` | `{config_hash, last_run, total, subscribed, skipped, failed}` | 人工/脚本核对订阅是否完整 |

调用的是 `POST /open-apis/drive/v1/files/<obj_token>/subscribe?file_type=docx`；只有文档**拥有者**能订阅，
重复调用幂等。`--verify` 时会先 `GET .../subscribe` 回查服务端 `is_subscribe`，以服务端为真值补订。

### 4.4 `wiki sync watch` 生成什么

| 产物 | 位置 | 内容 | 被谁消费 |
|------|------|------|----------|
| 重导后的文档 | `<local_dir>/<...>.md` | 被编辑的那一篇被**定向重导**（只重导命中节点，不重拉整棵） | 人 / RAG / Git |
| 变更记录 | `<local_dir>/.feishu-cli/changes/<YYYY-MM-DD>.jsonl` | 每条事件一行：`{file_token, obj_token, node_token, local_path, title, event_id, changed_at, status, error}`；`status` ∈ `updated / failed / missing` | 审计、diff、未来 `status` |

处理流程：长连接收 `drive.file.edit_v1`（或 `deleted_v1`）→ 解析 `file_token`（= `obj_token`）→
在索引反查 `node_token / local_path` → 按 `debounce` 聚合（同一文档窗口内只重导一次）→
用 `node_token` 走导出管线定向重导 → 写回 `.md` → 追加一条变更记录。`deleted_v1` 则记为 `missing`。

> watch 启动会默认做一次「订阅回查+补订」预检（`ReconcileSubscriptions`，`--no-precheck` 跳过），
> 确保开始监听前订阅关系是对的。本地 Target 文件不存在时 `.feishu-cli/changes/` 目录在首次重导时才创建。

### 4.5 `wiki sync reconcile` 生成什么

| 产物 | 位置 | 内容 | 被谁消费 |
|------|------|------|----------|
| 重导后的文档 | `<local_dir>/<...>.md` | 自基线后改动/新增的那几篇被**定向重导**（未变的跳过） | 人 / RAG / Git |
| 刷新后的索引 | `<local_dir>/.feishu-cli/wiki-index.json` | 改动/新增条目被重写；**未变条目也刷新元数据**（`obj_edit_time`/`title`/`parent_node_token` 等）；云端已消失的条目在 `clean: false` 时标 `sync_status: "gone"`、若目录改名则校正 `local_path` | RAG、`subscribe`、`watch` |
| 任务 manifest | `<local_dir>/.feishu-cli/manifests/<task>.json` | **以最终索引 + 磁盘资产为权威重建活集**（每个索引条目的 `.md` + 各 `.md` 的图片资产子目录），与旧清单做差剔除移动/删除后遗留的孤儿；`clean` 时对应旧文件被一并物理删除 | `clean`、结构变化检测 |
| 对账基线 | `~/.feishu-cli/state/wiki-sync/tasks/<task_hash>/last-reconcile.json` | `{task_id, wiki_url, local_dir, last_run, baseline, changed, gone, reexported, removed, skipped, failed}` | 下次 `reconcile` 读该任务基线做增量；`status` 展示；人工核对 |

对账原理：重新枚举整棵 → 用每个节点的 `obj_edit_time`（秒级 unix，毫秒自动折算）与基线比较 →
`> 基线` 或 **标题/父节点变化**的节点定向重导；解析失败退化为与索引字符串比对。首次无基线则视为全量（全部重导）。
`clean: true` 时云端已消失的节点会连带删除本地 `.md`；加上「孤儿清理」，移动/删除/重命名后不再被任何索引条目引用的旧 `.md` 与旧 assets 也会被一并删除（只删清单里记录过的该任务文件，绝不碰用户手工文件）。

### 4.6 `wiki sync status` 输出什么

只读聚合，不调 API、不写文件。顶层字段：`config / config_hash / state_dir / generated_at / subscriptions / queries[] / totals`。

每个 `queries[]` 项给出该任务的明细：

| 字段 | 含义 |
|------|------|
| `index.total / docx / subscribed / subscribe_failed / gone` | 索引条目数、docx 数、订阅成功/失败数、云端已消失（`sync_status=gone`）数 |
| `manifest.total_files / markdown` | 本任务实际写入的文件总数（含 assets）、Markdown 数 |
| `last_reconcile.ran / last_run / baseline / changed / reexported / removed / failed` | 上次对账是否跑过、时间、基线、该轮变更/重导/移除/失败数 |
| `events.records / updated / failed / missing / latest` | `.feishu-cli/changes/*.jsonl` 里的事件重导聚合与最近时间 |

顶层 `subscriptions` 来自 `state_dir/subscriptions.json`（上次 `subscribe` 汇总），`totals` 跨所有 query 求和。

```bash
# 默认 JSON；--jq 过滤、--format table 表格
feishu-cli wiki sync status --config ~/.feishu-cli/wiki-sync.yaml --jq '.queries[].index'
feishu-cli wiki sync status --config ~/.feishu-cli/wiki-sync.yaml --format table --jq '.queries[] | {name, subscribed: .index.subscribed}'
```

---

## 5. 消费关系速查（谁读什么）

| 产物 | 写入命令 | 读取命令 |
|------|----------|----------|
| `<local_dir>/.feishu-cli/wiki-index.json` | `pull`、`subscribe`（回写）、`watch`（预检回写）、`reconcile`（刷新） | `subscribe`、`watch`、`reconcile`、`status`、RAG |
| `<local_dir>/.feishu-cli/manifests/<task>.json` | `pull`、`reconcile`（追加/剔除） | `reconcile`（clean 判定）、`status`（文件数统计） |
| `~/.feishu-cli/state/wiki-sync/<cfg_hash>/subscriptions.json` | `subscribe` | `status`、人工对账 |
| `~/.feishu-cli/state/wiki-sync/tasks/<task_hash>/last-reconcile.json` | `reconcile` | 下次 `reconcile`（该任务增量基线）、`status`、人工核对 |
| `<local_dir>/.feishu-cli/changes/<date>.jsonl` | `watch` | `status`（事件聚合）、审计、Git |
| `<local_dir>/**/*.md`、`<local_dir>/assets/**` | `pull`、`watch`、`reconcile` | RAG、Git、人 |

---

## 6. 端到端示例（替换占位符后可跑）

```bash
# 0) 授权（首次）
feishu-cli auth login --domain "知识库" --recommend

# 1) 写配置
cat > ~/.feishu-cli/wiki-sync.yaml <<'YAML'
options:
  debounce: 10s
  download_images: true
queries:
  - name: "叉车取货文档"
    wiki_url: "https://example.feishu.cn/wiki/WIKI_NODE_TOKEN"
    local_dir: "./doc_sop_test"
YAML

# 2) 全量拉取
feishu-cli wiki sync pull --config ~/.feishu-cli/wiki-sync.yaml
# 产物: ./doc_sop_test/*.md, ./doc_sop_test/.feishu-cli/wiki-index.json, ./doc_sop_test/.feishu-cli/manifests/<hash>.json

# 3) 批量订阅
feishu-cli wiki sync subscribe --config ~/.feishu-cli/wiki-sync.yaml
# 产物: 索引回写 subscribe_status, + 全局 subscriptions.json

# 4) 常驻监听（Ctrl-C 退出）
feishu-cli wiki sync watch --config ~/.feishu-cli/wiki-sync.yaml
# 产物: 被改文档定向重导 + .feishu-cli/changes/<date>.jsonl
# 或者用 4') 定时对账（替代 watch，无需常驻进程，cron 每日跑一次）
#    feishu-cli wiki sync reconcile --config ~/.feishu-cli/wiki-sync.yaml
```

---

## 7. 已实现 / 尚未实现（Phase 4 范围）

已实现：
- `wiki sync reconcile`：增量/定时对账（按 `obj_edit_time` 与基线比对）。已覆盖**内容变更**与**结构变化**
  （改名/移动按 `title`/`parent_node_token` 识别；删除/移出/回收站按节点从枚举中消失识别），
  并支持 `clean` 严格镜像：`clean: true` 时云端已消失的文件从磁盘 + 清单剔除，`clean: false` 时保留但标 `gone`。
- `wiki sync status`：只读聚合各任务的索引/订阅/对账基线/事件记录状态（不调 API 不写文件，支持 `--format`/`--jq`）。

尚未实现（勿按此操作）：
- 全局 `tasks.json`、`event-queue.ndjson`、`events/<date>.ndjson`、日志文件目前**未写入**。

当前 `wiki sync` 有 `pull`、`subscribe`、`watch`、`reconcile`、`status` 五个子命令。

---

## 8. 隐私与占位符要求

- 仓库内**禁止**出现真实企业前缀域名（如 `<企业>.feishu.cn` 这类带租户前缀的域名）、真实 App Secret、
  真实 User Token、真实个人邮箱；URL 用通用 `feishu.cn`，token 用 `cli_xxx`。
- 实际测试可用临时配置文件（如 `/tmp/wiki-sync.yaml`），内容含真实域名仅限本地，勿提交。
- `.env`、`config.yaml`、`token.json`、`wiki-sync.yaml` 含敏感信息，已由 `.gitignore` 排除，禁止提交。
