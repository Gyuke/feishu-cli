# 知识库增量同步使用指南（`wiki sync pull / subscribe / watch / reconcile / status`）

`feishu-cli wiki sync` 命令组的**使用文档**：从「把云端知识库拉取到本地（指定保存目录）」、
「建立事件订阅」到「常驻监听文档改动并定向重导」与「定时全量对账」的完整流程。

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
「一个 wiki_url → 一个本地保存目录」。

```yaml
options:
  debounce: 10s            # 同一文档两次定向重导的最小间隔（watch 用）
  download_images: true    # 拉取时下载图片到 assets_dir
  clean: false             # 是否严格镜像：云端删了本地是否也删（reconcile 用；false=保留+索引标 gone）
  continue_on_error: true  # 单个节点失败是否继续
  include_types: [docx, sheet]

queries:
  - name: "叉车取货文档"
    wiki_url: "https://example.feishu.cn/wiki/WIKI_NODE_TOKEN"   # 根节点 URL（必填）
    local_dir: "./doc_sop_test"                                  # 保存目录（必填，对应旧 -o <dir>）
    # assets_dir: "./doc_sop_test/assets"   # 省略则默认 <local_dir>/assets
    # conflict: "overwrite"                 # overwrite | skip | fail
    # debounce: "10s"                        # 覆盖 options 层
```

关键字段：

| 字段 | 必填 | 说明 |
|------|------|------|
| `wiki_url` | 是 | 知识库节点 URL。`task_hash = hex(sha256(wiki_url))` 由此派生（任务身份，不随目录移动变） |
| `local_dir` | 是 | 本地保存目录（对应旧命令的 `-o <dir>`） |
| `assets_dir` | 否 | 图片资源目录，默认 `<local_dir>/assets` |
| `include_types` | 否 | 导出类型，默认 `docx, sheet`（watch 只对 `docx` 订阅/重导） |
| `debounce` | 否 | 事件聚合窗口，默认 `10s` |

> `local_dir` / `assets_dir` 支持 `~/`（如 `~/Documents/seer_docs/...`），会在解析时展开为
> 用户主目录，避免被当作字面量目录名。也可以直接用绝对路径（`/home/<user>/...`）或相对路径。

---

## 3. 完整命令（拉取 + 订阅 + 变更→重导）

```bash
# ① 全量拉取到本地（生成 Markdown + 索引 + manifest + legacy map）
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
基线目录按**任务身份**（`local_dir + wiki_url`）派生，与配置文件哈希**解耦**：改 `clean`、加注释、改其它字段都**不会**重置基线、触发全量；只有真正换任务（`wiki_url` 或 `local_dir` 变）才需要重来。`<task_hash> = hex(sha256(local_dir + "\x00" + hex(sha256(wiki_url))))`。

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

## 4. 端到端示例

```bash
# 0) 授权（首次）
feishu-cli auth login --domain "知识库" --recommend

# 1) 写配置
cat > ~/.feishu-cli/wiki-sync.yaml <<'YAML'
options:
  debounce: 10s
  download_images: true
queries:
  - name: "测试文档"
    wiki_url: "https://example.feishu.cn/wiki/WIKI_NODE_TOKEN"
    local_dir: "./docs_test"
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