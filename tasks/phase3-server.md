你是执行者，这份文档是你唯一的任务来源；中途没人可问，拿不准的写进 `BLOCKED.md`（待裁决清单），跳过继续做别的，最后随交付一起提交。断了或换新会话：先读 `PROGRESS.md` 接着做，别重做。

这活为什么干：把 Sender 的授权从"设备码输码"改成 Google/Claude Code 式的"授权链接 + 同意页"，同时按选定设计重做整套 Web UI，并加上历史记录页（过滤 + 全文搜索 + 统计）。干完世界什么样：agent 或手机 App 发起授权时拿到一个 /authorize 链接，用户点开、登录、在 sudo 式同意页点批准，agent 拿 code 换 token；网页变成等宽字体琥珀色的终端风，登录后能浏览全部历史、grep 式搜索、看趋势统计。设备码流程整个删掉。让步顺序：现有功能不回退 > 授权流程正确 > 视觉贴合设计稿 > 其余。「只允许/不许」是死规矩，违反算失败；「建议」有更好的路就走，在 `PROGRESS.md` 记一句为什么。

**我替领导拍的板**（领导已确认：方向 2 Terminal 设计、授权码双回传、历史三件套、设备码移除）

* 视觉规格：`design/draft-2-terminal.html`（只读，不许改）——全站等宽字体、琥珀 accent（#E8A33D）、日志行列表、`$` 提示符过滤行、sudo 式同意页。

* 页面结构（用户修正已确认）：**独立页无站点导航、仅链接直达**——`/login`、`/register`、`/authorize`（同意页）、OOB 码页；**应用内页带顶部导航**（概览 / 历史 / 绑定设备）。

* OAuth 授权码 + PKCE（S256，强制）：`GET /authorize?response_type=code&client_id&redirect_uri&code_challenge&code_challenge_method=S256&state`；未登录跳 `/login?next=…`；同意 → 302 `redirect_uri?code=…&state=…`，拒绝 → `redirect_uri?error=access_denied&state=…`。code：32 hex、TTL 300 秒、单次使用、服务端存 sha256，绑定 client\_id/redirect\_uri/code\_challenge/user\_id。

* `redirect_uri` 白名单规则：`http(s)://localhost[:任意端口]/*`、`http(s)://127.0.0.1[:任意端口]/*`、任意 `https://*`、任意非 http 自定义 scheme（手机 App 用 `sender://`）、以及 OOB 兜底 `urn:ietf:wg:oauth:2.0:oob`（渲染"一次性 code 复制页"）。其余拒绝。

* `POST /api/v1/oauth/token` 只认 `grant_type=authorization_code`（code+code\_verifier+client\_id+redirect\_uri），校验 PKCE 与绑定关系后发 access\_token（32 hex、7 天、存哈希）；错误映射 RFC 6749：invalid\_request/invalid\_grant/invalid\_client。

* 设备码清理：删 `device_authorization` 端点（404）、device\_code grant 逻辑、user\_code 页面与测试；迁移 `DROP TABLE IF EXISTS device_grants`（生产库该表无数据）。`/api/v1/oauth/device_authorization` 必须 404。

* `POST /api/v1/devices/bind` 鉴权改为**会话 cookie 或 Bearer 访问令牌二选一**，成功返回 `{"ok":true,"username":"…"}`（安卓 App 用 token 绑定）。

* 历史页：日期/app/设备过滤 + FTS5 全文搜索（content/sender/chat）+ 页码分页（OFFSET，每页 50，显示共 N 条）；搜索词做 FTS 转义（双引号包裹、内部引号翻倍），搜索为空返回全部。API 的 cursor 翻页不动。

* 概览统计：今日/本周条数、已授权 agent 数、上行状态、7 日趋势（COUNT GROUP BY day）、App 占比——全部 SQL 聚合，服务端渲染。

* FTS5 与旧库：`messages_fts` 虚表（content='messages'、content\_rowid='id'）+ 同步触发器 + 存量回填；任务 0 先验证 modernc.org/sqlite 的 FTS5 可用。

* 生产库 `data/messages.db`（真实数据，生产服务正跑旧二进制在 :8080）只许增量迁移，测试一律临时库。

**界限**

* 白名单：只允许改/建 `server/` 下 `store.go`、`http.go`、`web.go`、`main.go`、`server_test.go`、`README.md`、`go.mod`/`go.sum`、`templates/*.html`。`design/`、`tasks/`、`android/`、仓库根 `data/` 只读，碰都不许碰。

* 现有测试：设备码相关测试（device\_authorization/pending/slow\_down/denied/expired\_token/device grant 类）可删；其余断言不许改。

* 顺手活（重构、加依赖）进 `BLOCKED.md` 待裁决。

**现状与任务 0**
实测（2026-08-03）：`go test ./...` 43 个全绿、0 skip；生产库 70 条消息、1 台设备（未绑定）。任务 0：跑 `go test ./...` 确认 43 绿；跑 `CREATE VIRTUAL TABLE` FTS5 冒烟确认可用（不行就写 `BLOCKED.md` 并改用 LIKE 兜底，注明理由）；把「理解的目标/顺序/最大风险」≤10 行写进 `PROGRESS.md` 再动工。

**任务**

1. 迁移：删 device\_grants、建 oauth\_codes（code\_hash PK、client\_id、user\_id、redirect\_uri、code\_challenge、expires\_at、used\_at）、建 messages\_fts + 触发器 + 回填。验收：临时旧库迁移后消息数不变、device\_grants 消失、oauth\_codes/messages\_fts 在。
2. 授权码端点：`GET /authorize`（参数校验 → 登录门 → 同意页 → 302/OOB 页）、`POST /api/v1/oauth/token`（authorization\_code + PKCE + 单次）；删除设备码全部逻辑。验收：设备码端点 404。
3. bind 双鉴权：会话或 Bearer token，返回 username。
4. Web UI 重设计（按 design/draft-2-terminal.html）：应用内页（概览：KPI+7 日趋势+tail -5 日志；历史：`$` 过滤行+日志列表+分页；绑定设备页）+ 独立页（login/register/authorize sudo 同意页/OOB 码页），模板统一 embed；概览与历史接真实 SQL。
5. 历史页能力：过滤（day/app/device）、FTS 搜索（转义）、分页（共 N 条）。
6. 测试：删设备码测试；新增 ≥16——授权码全流程（authorize→登录→同意→code→token→读 200）、PKCE 不匹配、code 复用、code 过期（负 TTL）、redirect\_uri 不符、client\_id 不符、state 透传、OOB 页出码、loopback 放行、自定义 scheme 放行、http 非本机拒绝、未登录跳登录、拒绝→error=access\_denied、缺 code\_challenge 拒绝、bind 带 token 返回 username、FTS 命中与转义、历史页 200、设备码端点 404。总数 ≥51、0 skip。
7. README：agent 接入指南重写（授权链接 + 两种回传写法）、删设备码文档、bind 双鉴权、历史页说明。
8. 收尾：`go build`、`go test` 全绿、`docker build` 成功；旧库迁移验证贴输出。

**规矩**

* 防作弊点名：删/改非设备码测试断言、skip、放宽断言、mock 被测对象、`|| true` 全算失败；测试数只许 ≥51。

* 同一验收连败 3 次换下一项；结果比开工差就回滚如实报告——「没做成但说清了」合格，「做了但更糟」不合格。

**完成条件**

* 硬指标一：`go test ./...` 全绿 ≥51、0 skip；对话贴 curl 冒烟：authorize 全流程（登录→sudo 同意→拿 code→换 token→带 token 读 200）、OOB 页渲染出 code、拒绝回 error=access\_denied、`device_authorization` 404、bind 带 Bearer token 成功且返回 username。

* 硬指标二：旧库迁移消息数不变（贴输出）；`docker build` 通过；`GET /history`（登录会话）200 且含真实消息行。

* 每条验收贴实际命令输出，只说做完了不算。`BLOCKED.md` 随交付提交，空的也写「无」。止损：或已跑满 6 轮——满轮即停，如实汇报卡在哪、还差什么。

