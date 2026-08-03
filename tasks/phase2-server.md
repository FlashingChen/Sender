你是执行者，这份文档是你唯一的任务来源；中途没人可问，拿不准的写进 `BLOCKED.md`（待裁决清单），跳过继续做别的，最后随交付一起提交。断了或换新会话：先读 `PROGRESS.md` 接着做，别重做。

这活为什么干：给 sender-server 加上账号和 OAuth 设备码授权，并给设备上报加一道账号绑定门槛。干完世界什么样：用户在网页注册账号；agent 拿到网址后发起设备码授权，用户在网页登录输码，agent 轮询拿 token 后带 token 查数据；手机上报数据前必须被账号在网页上绑定过，陌生设备传不进来。阶段一功能全部保留。让步顺序：现有功能不回退 > OAuth 流程正确 > 上报门槛生效 > UI 美观。「只允许/不许」是死规矩，违反算失败；「建议」有更好的路就走，在 `PROGRESS.md` 记一句为什么。

**我替领导拍的板**（user_code=网页输入的 8 位码，device_code=agent 轮询用的码；默认值我定的，错了代价小）
- 单任务书全包（账号+OAuth+WebUI+绑定都在 `server/` 一个 Go 模块，拆开必踩）。
- OAuth 标准设备码（RFC 8628）。新端点：`POST /api/v1/oauth/device_authorization`、`POST /api/v1/oauth/token`，都同时接受 JSON 和表单 body，响应一律 JSON。
- 任意非空 `client_id` 可发起授权（公共客户端，agent 零预注册）；`scope` 忽略；风险由用户输码确认兜底。
- user_code 8 位随机（去 0/O/1/I），展示 `ABCD-EFGH`；device_code 32 hex；expires_in 600 秒；interval 5 秒，轮询过快返回 `slow_down`；access_token 32 hex、7 天有效、库中只存 sha256 哈希；会话 cookie HttpOnly+SameSite=Lax、7 天、库中存哈希。
- 密码 bcrypt（唯一新依赖 `golang.org/x/crypto`），用户名唯一、密码≥8 位。
- 读接口改造：`GET /messages`、`GET /apps` 只认用户 Bearer token，无/坏一律 401（阶段一裸读→阶段二必须授权，有意的变更）；写接口（register/upload）仍用设备 secret，安卓端流程不动。
- 上报门槛（新拍板）：`devices` 表加 `user_id`（可空）+`bound_at`；上传时设备未绑定→403；绑定=登录后在 Web UI 凭 device_id+secret 提交（证明手机在你手里）。安卓端设置页会新增显示 device_id/secret（另一本小书，并行）。
- 注册开关：环境变量 `ALLOW_REGISTRATION`（默认 true）；false 时注册接口 403。README 写明"注册完自己账号后设 false"。
- Web UI：Go html/template 中文界面、零外部资源（CSS 内联、模板 embed 进二进制）；页面：首页、注册、登录、登出、授权页（输码→展示 client_id→确认/拒绝）、绑定设备页；未登录访问授权页/绑定页跳登录；未知码提示"码不存在或已过期"。
- 过期数据不主动清理，查询时判过期。
- 生产库 `data/messages.db`（有真实数据，生产服务器正用旧二进制跑在 :8080，别动它）只许增量迁移；测试一律临时库。

**界限**
- 白名单：只允许改/建 `server/` 下 `store.go`、`http.go`、`main.go`、`server_test.go`、`README.md`、`go.mod`/`go.sum`，新建 `oauth.go`、`web.go`、`templates/`。其余只读，`android/`、仓库根 `data/`、`tasks/` 碰都不许碰。
- 现有 25 个测试：断言一行不许改；唯一例外是测试 helper `registerTestDevice` 可以加"绑定设备"步骤（否则全流程测试无法通过），并新增"未绑定 403"用例兜住。
- 顺手活（重构、加 UI 动效、装新依赖）进 `BLOCKED.md` 待裁决。

**现状与任务 0**
实测（2026-08-03）：`go test ./...` 25 个全绿、0 skip；翻页修复完成（cursor 为 `ts:id` 复合键）；生产库含真实消息。任务 0：跑 `go test ./...` 确认 25 绿；`go get golang.org/x/crypto` 确认依赖可下载；把「理解的目标/顺序/最大风险」≤10 行写进 `PROGRESS.md` 再动工。

**任务**
1. 数据层：新增 `users`（id, username 唯一, password_hash, created_at）、`sessions`（token_hash 唯一, user_id, expires_at）、`device_grants`（device_code 唯一, client_id, user_code 唯一, status[pending/approved/denied], user_id, created_at, expires_at, last_poll_at）、`tokens`（token_hash 唯一, user_id, expires_at）；`devices` 加列 user_id/bound_at（列不存在才加，否则旧库起不来）。store 方法：注册/取用户/建验会话/建 grant/按 user_code 找 grant/批准拒绝/按 device_code 找 grant/签发验 token/记轮询/绑定设备/查设备绑定状态。
2. OAuth 端点：`device_authorization`（client_id 必填；verification_uri 用请求 Host 拼 `http(s)://host/authorize`，有 `X-Forwarded-Proto` 则用它；返回 verification_uri/verification_uri_complete/user_code/device_code/expires_in/interval）；`token`（grant_type=urn:ietf:params:oauth:grant-type:device_code 必填；状态机：不存在/过期→expired_token、pending→authorization_pending、denied→access_denied、approved→发 token；轮询间隔<interval→slow_down；client_id 不符→400）。
3. 读接口鉴权改造：`GET /messages`、`GET /apps` 校验 Bearer 用户 token，失败 401。
4. 上报门槛：upload 端点鉴权通过后检查设备已绑定（user_id 非空），未绑定 403 `{"error":"device not bound"}`；`POST /api/v1/devices/bind`（Web 会话鉴权，JSON/表单：device_id+secret，secret 不符 400）；注册端点受 `ALLOW_REGISTRATION` 控制（false 时 403）。
5. Web UI（中文）：注册/登录/登出/首页/授权页/绑定设备页；表单校验（用户名非空、密码≥8）；授权页确认/拒绝按钮；绑定页错误提示。
6. 测试（新增 ≥15、总数 ≥40、0 skip）：注册成功/重名失败、登录对/错密码、未登录访问授权页跳登录、授权全流程（请求→登录批准→轮询拿 token→带 token 查 messages 200）、authorization_pending、access_denied、expired_token、slow_down、无 token 401、坏 token 401、token 过期 401（负 TTL 造过期 token）、未绑定上传 403、绑定后上传 200、绑定 secret 错 400、ALLOW_REGISTRATION=false 注册 403。现有 25 个继续全绿。
7. README：OAuth 两端点文档、读接口鉴权变化、绑定设备说明、`ALLOW_REGISTRATION` 说明、新增「给 agent 的接入说明」（一句话+4 步：拿授权→用户输码→轮询 token→带 token 查）。
8. 收尾：`go build`、`go test` 全绿、`docker build` 成功（模板已 embed，Dockerfile 不用改）；旧库迁移验证：造含旧数据的临时库→跑迁移→消息数不变，贴输出。

**规矩**
- 防作弊点名：删/改现有断言、skip、放宽断言、mock 被测对象、`|| true` 全算失败；测试数只许 ≥40；现有 25 个断言不动（helper 例外已写明）。
- 不新增流程、权限、依赖，必须加的写 `BLOCKED.md`（bcrypt 已拍板，不算）。
- 同一验收连败 3 次换下一项；结果比开工差就回滚如实报告——「没做成但说清了」合格，「做了但更糟」不合格。

**完成条件**
- 硬指标一：`go test ./...` 全绿、≥40、0 skip；对话贴全流程冒烟输出：device_authorization 请求→注册+登录→批准→轮询拿 access_token→带 token `GET /messages` 200、不带 401；未绑定设备上传 403→绑定后 200。
- 硬指标二：旧库迁移不丢数据（贴输出）；`go build`、`docker build` 通过。
- 每条验收贴实际命令输出，只说做完了不算。`BLOCKED.md` 随交付提交，空的也写「无」。止损：或已跑满 6 轮——满轮即停，如实汇报卡在哪、还差什么。
