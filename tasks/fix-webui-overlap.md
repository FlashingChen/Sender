你是执行者，这份文档是你唯一的任务来源；中途没人可问，拿不准的写进 `BLOCKED.md`（待裁决清单），跳过继续做别的，最后随交付一起提交。断了或换新会话：先读 `PROGRESS.md` 接着做，别重做。

这活为什么干：修复 Web UI 两个显示问题——①历史页/概览页的包名（如 com.twitter.android）溢出压进消息列导致文字重叠；②消息内容被 UI 强制单行省略，全文永远看不到。干完世界什么样：src 列显示短标签（X / 微信 / Android 系统），包名悬停可见，任何宽度下不重叠；历史页点一行展开/收起完整消息（含换行），概览 tail 同理。采集层截断（通知预览长度限制）不修，只写清限制。让步顺序：不重叠 > 全文可看 > 其余。「只允许/不许」是死规矩，违反算失败；「建议」有更好的路就走，在 `PROGRESS.md` 记一句为什么。

**我替领导拍的板**

* 重叠病根（已实测确认）：`.line` 三列固定 `150px 110px 1fr`，src 列渲染原始包名（17 字符 ≈ 136px > 110px），且无溢出处理 → 文字压进消息列。同时 `.line` 的 grid 子项缺 `min-width:0`，长内容必然溢出。

* 修法：src 列改渲染 `AppName`（短标签），`title` 属性放包名；`styles.html` 给 `.line` 加 `min-width:0`，`.line .src` 加 `overflow:hidden;text-overflow:ellipsis;white-space:nowrap`，`.line .m` 保持单行省略（展开时 `white-space:pre-wrap`）。

* 全文展开：history.html 每行加 `data-full` 语义（行内已含全文，CSS 控制即可，无需再请求）；内联 JS（≤15 行，无依赖）：点击 `.line` 切换 `expanded` class，展开时 `.m` 用 `white-space:pre-wrap` 显示全部内容（含 \n 换行）。概览页 tail -5 用同一套 CSS/JS。

* App 占比列表同样渲染 `AppName`（title 放包名）。

* 采集层截断是通知监听固有限制（微信约几十字、X 通知约 280 字），服务端存的就是手机采到的——不修，在 `server/README.md` 已知限制里写明，让用户知道长消息不全不是丢数据。

* 历史页 `dev=` 下拉已显示 device\_id（长 UUID），允许省略号截断（title 全文），不算 bug。

**界限**

* 白名单：只允许改 `server/templates/history.html`、`server/templates/home.html`、`server/templates/styles.html`（或 layout.html 里的样式块）、`server/README.md`。其余只读，`android/`、`design/`、`tasks/`、`data/` 碰都不许碰。

* 现有 53 个测试一个不许改、不许删；本修复不改 Go 逻辑，若发现必须改逻辑才能修，写 `BLOCKED.md` 停下。

**现状与任务 0**
实测（2026-08-03）：`go test ./...` 53 个全绿、0 skip；生产库 71 条消息，最长 272 字符（X 通知截断），包名 `com.twitter.android` 在 110px 列必然溢出。任务 0：跑 `go test ./...` 确认 53 绿；打开 `server/templates/history.html` 与 `styles.html` 确认当前结构；把「理解的目标/顺序/最大风险」≤10 行写进 `PROGRESS.md` 再动工。

**任务**

1. `styles.html`：`.line` 加 `min-width:0`；`.line .src` 加三件套（overflow hidden / text-overflow ellipsis / white-space nowrap）；`.line.expanded .m` 加 `white-space:pre-wrap`；`.line` 加 `cursor:pointer`（仅 history/home 的 .line）。
2. `history.html`：src 列改 `{{.AppName}}` + `title="{{.App}}"`；行加可点击展开 JS（toggle expanded class，展开/收起切换）；消息列结构不变。
3. `home.html`：tail -5 的 src 列同样改 `{{.AppName}}` + title；App 占比列表改 `{{.AppName}}` + title="包名"；接同一段展开 JS。
4. `README.md`（server 的）：已知限制补一条——「通知预览长度限制：微信约几十字、X 约 280 字，超长消息在采集时就被截断，服务端存的就是手机采到的原文；需完整文案可考虑无障碍方案（未做）」。
5. 收尾：`go build ./...`、`go test ./...` 全绿（仍 53、0 skip），贴输出。

**规矩**

* 防作弊点名：删/改测试、skip、放宽断言、`|| true` 全算失败；测试数只许 ≥53。

* 同一验收连败 3 次换下一项；结果比开工差就回滚如实报告——「没做成但说清了」合格，「做了但更糟」不合格。

**完成条件**

* 硬指标一：`go test ./...` 全绿 ≥53、0 skip，`go build` 过，输出贴在对话里；`history.html`/`home.html` 中可 grep 到 `{{.AppName}}` + `title=` 与展开 JS。

* 硬指标二：`styles.html` 可 grep 到 `.line .src` 的 ellipsis 三件套与 `.line.expanded .m` 的 `pre-wrap`。

* 每条验收贴实际命令输出，只说做完了不算。`BLOCKED.md` 随交付提交，空的也写「无」。止损：或已跑满 4 轮——满轮即停，如实汇报卡在哪、还差什么。

