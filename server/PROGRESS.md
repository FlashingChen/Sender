# Sender CLI + loopback 授权（用户直接需求）
- 目标：给 agent 一个 CLI（`server/cmd/sender`，模块内 stdlib-only），替代手写 HTTP；OAuth 流程改为 RFC 8252 loopback——CLI 绑定 127.0.0.1 随机端口，浏览器点「批准」后 302 回本地回调，CLI 直接换 token，不再需要复制粘贴 OOB code。
- 范围：服务端零改动（loopback redirect_uri 本就在白名单，TestOAuthLoopbackAllowed 已覆盖）；OOB 流程保留作无浏览器环境的兜底。
- 实现：`sender login/logout/status/messages/apps`；默认 JSON 输出（agent 可直接消费），`--text` 人类表格；`--no-browser` 打印授权链接（SSH 场景）；凭证存 `~/.config/sender/config.json`（0600）；支持 `SENDER_SERVER`/`SENDER_TOKEN`（无状态调用）/`SENDER_CONFIG`。
- 验证：全量 `go test ./...` 75 绿 0 skip（53 服务端 + 22 CLI）；E2E 冒烟用真实服务端 + curl 模拟浏览器完成「登录→同意页→批准→loopback 回调→换 token→查消息/App 统计→登出」全链路。
- 边界：AppShare 的 AppName 渲染问题仍按 `tasks/fix-appshare-label.md` 单独处理（未动 store.go）。

# Phase 3 server progress（原文）
- Goal: replace device-code OAuth with authorization-code + PKCE, terminal UI, and searchable history.
- Order: baseline/FTS, additive migration, OAuth endpoints, bearer/session bind, Web UI and SQL-backed history, tests/docs, full smoke.
- Boundaries: only the server whitelist changes; production `data/messages.db` is read-only and receives additive migration only.
- Compatibility: preserve existing message/API behavior and cursor pagination; remove every device-code endpoint, grant, page, and test.
- Maximum risks: PKCE/code single-use and redirect validation; legacy SQLite migration/FTS triggers; session versus bearer authentication.
- Baseline: `go test ./...` passed (1 package ok, 1 no tests); temporary modernc SQLite `CREATE VIRTUAL TABLE ... USING fts5` printed `fts5 ok`.
- Verification: `go test -json ./...` counted 53 passing test cases with no explicit skips; curl covered OAuth/OOB/deny/404/bearer bind/history; Docker build passed; production copy migrated 73→73 messages, dropped `device_grants`, and added `oauth_codes`/`messages_fts`.
- Status: implementation complete. FTS backfill uses idempotent FTS5 `rebuild`; consent callback uses 302 Found per RFC 6749.

# WebUI 重叠修复（fix-webui-overlap）
- 目标：历史页/概览页 src 列显示 AppName 短标签（title 悬停包名），任何宽度不重叠；点击行展开/收起完整消息（含换行）；采集层截断不修，README 已知限制写明。
- 顺序：基线测试（已绿）→ layout.html 样式块 → history.html → home.html → README → build/test/grep 验收 → BLOCKED.md。
- 最大风险：bind.html 复用 .line/.src（device UUID），cursor:pointer 只能放 history/home 页内样式，不能进共享 .line 规则；.m 展开后仍带 overflow:hidden，无空格超长 token 会被裁剪（采集层上限内，可接受）。
- 偏离说明：文档字面只在 .line 容器加 min-width:0 不解决 grid 子项收缩（子项默认 min-width:auto），故另加 `.line .t,.line .src,.line .m{min-width:0}` 落实病根；.src 的 overflow:hidden 也使子项自动最小尺寸归零。
- BLOCKED：App 占比列表渲染 AppName 需 store.go 的 AppShareRow 加字段并改查询 SELECT/scan（越白名单、改 Go 逻辑），模板层无数据可用，已列入 BLOCKED.md。
