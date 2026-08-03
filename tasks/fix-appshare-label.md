你是执行者，这份文档是你唯一的任务来源；中途没人可问，拿不准的写进 `BLOCKED.md`（待裁决清单）。断了或换新会话：先读 `PROGRESS.md` 接着做，别重做。

这活为什么干：收尾上次 WebUI 修复的遗留裁决——概览页「App 占比」列表仍显示原始包名（com.twitter.android 等），和刚修好的历史页/tail 不一致。干完世界什么样：概览页 App 占比也显示短标签（X / 微信 / Android 系统），包名悬停可见。这是领导对 `BLOCKED.md` 里那条裁决的批复：**允许动 store.go**（上次不许改 Go 逻辑的限制仅针对上本书，本书已豁免）。让步顺序：不破坏现有测试 > 显示一致 > 其余。「只允许/不许」是死规矩，违反算失败。

**我替领导拍的板**
- `store.go` 的 `AppShareRow` 加 `AppName string` 字段；概览聚合 SQL 的 SELECT 加 `MAX(m.app_name)`（或 m.app_name，按现有分组写法取其一），scan 时填入；现有 53 个测试不许改，若 SQL 改动导致现有概览相关测试失败，停下写 `BLOCKED.md`（先自查是不是 JOIN 语义问题）。
- `home.html` 的 App 占比行改为 `<span title="{{.App}}">{{.AppName}}</span>`。
- `BLOCKED.md` 里那条裁决记录追加一行「领导裁决 2026-08-03：允许 store.go 加 AppName 字段，见本任务书」。
- 不新增依赖、不改其他页面。

**界限**
- 白名单：只允许改 `server/store.go`、`server/templates/home.html`、`server/BLOCKED.md`、`server/PROGRESS.md`。其余只读。
- 现有 53 个测试一个不许改、不许删。

**现状与任务 0**
实测（2026-08-03）：`go test ./...` 53 个全绿、0 skip；`store.go` 962 行附近有 `AppShareRow`（App/Count/Pct 三字段），概览 SQL 在 980 行附近的 `Overview` 聚合里。任务 0：跑 `go test ./...` 确认 53 绿；把「理解的目标/顺序/最大风险」≤10 行写进 `PROGRESS.md` 再动工。

**任务**
1. `store.go`：`AppShareRow` 加 `AppName`；聚合查询 SELECT 带 app_name；scan 填充。
2. `home.html`：App 占比 `<span title="{{.App}}">{{.AppName}}</span>`。
3. 收尾：`go build ./...` + `go test ./...` 全绿（仍 53、0 skip），贴输出；`BLOCKED.md` 裁决记录追加。

**规矩**
- 防作弊点名：删/改测试、skip、放宽断言、`|| true` 全算失败；测试数只许 ≥53。
- 同一验收连败 3 次换下一项；结果比开工差就回滚如实报告。

**完成条件**
- 硬指标一：`go test ./...` 全绿 ≥53、0 skip，`go build` 过，输出贴在对话里。
- 硬指标二：`home.html` 可 grep 到 `title="{{.App}}"` 与 `{{.AppName}}` 同现于 App 占比行；`store.go` 可 grep 到 `AppShareRow` 的 `AppName` 字段。
- 每条验收贴实际命令输出，只说做完了不算。`BLOCKED.md` 随交付提交。止损：或已跑满 3 轮——满轮即停，如实汇报卡在哪、还差什么。
