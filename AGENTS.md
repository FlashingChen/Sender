# AGENTS.md — Sender

## 项目一句话
自托管个人通知采集系统：安卓端采集通知（发送者+内容+时间）→ Go 服务端存储 → Web 控制台、OAuth API 与 `sender` CLI（agent 直接查数据，loopback 授权免粘贴 code）。

## 怎么跑
- 服务端：`cd server && go run ./cmd/server`（监听 :8080，数据存 data/messages.db）；生产用 `./dist/sender-server` 或 `cd server && docker compose up -d --build`。
- CLI：`cd server && go build -o dist/sender ./cmd/sender`；`sender login` 走 loopback 授权（浏览器点批准即完成），`sender messages/apps` 输出 JSON。
- 环境变量：`ADDR`（默认 `:8080`）、`DB_PATH`、`TZ`（默认 `Asia/Shanghai`）、`ALLOW_REGISTRATION`（注册完自己的账号后设 `false`）；CLI 另支持 `SENDER_SERVER` / `SENDER_TOKEN` / `SENDER_CONFIG`。
- 服务端门禁：`cd server && go build ./... && go test ./...`（必须 ≥53 全绿、0 skip；不许删/改测试降级）。
- 安卓：`cd android && ./gradlew assembleDebug`；单测 `./gradlew testDebugUnitTest`（当前 40 绿 0 skip）。JDK 17+，compileSdk 35 / targetSdk 34 / minSdk 26。

## 技术栈
- 服务端：Go（modernc.org/sqlite 纯 Go、含 FTS5），内嵌模板 Web UI，bcrypt 密码，OAuth 授权码 + PKCE。
- 安卓：Kotlin + Compose、Room、WorkManager、NotificationListenerService；Gradle 8.13 / AGP 8.7.3 / Kotlin 2.0.21。

## 目录与约定
- `server/` 服务端（`cmd/server` 主服务，`cmd/sender` 为 agent CLI）；`android/` 采集端；`design/` Web UI 设计定稿（direction-approved.md + draft-2-terminal.html）；`tasks/` 任务书；`data/` 生产数据库。
- `data/`、`dist/`、`*.db*` 已 gitignore，永不入库（含真实消息与设备密钥）；备份 = 复制 data/messages.db。
- 上报契约：`client_msg_id = 包名:通知key:postTime毫秒` 幂等去重；单批 ≤500；2xx 才标 synced。
- 采集层截断是固有限制（微信约几十字、X 约 280 字），服务端存的就是手机采到的原文。

## 当前状态与下一步
- 阶段一~三完成并已上线（本机 `./dist/sender-server` 运行中，/healthz OK）；origin/main 与本地一致。
- 已交付：`server/cmd/sender` CLI（login 走 RFC 8252 loopback 授权，浏览器点「批准」即完成，无需粘贴 code；messages/apps/status/logout，默认 JSON 输出；`SENDER_SERVER`/`SENDER_TOKEN`/`SENDER_CONFIG` 环境变量），全量测试 75 绿 0 skip。
- 下一步：执行 `tasks/fix-appshare-label.md`（概览页 App 占比渲染 AppName，领导已裁决允许动 store.go）；执行时按任务书更新 server/BLOCKED.md 与 server/PROGRESS.md。
