# BLOCKED（待裁决清单）

## 1. App 占比列表渲染 AppName（home.html 任务 3 末项）
- 现状：home.html App 占比行渲染 `{{.App}}`（包名原文），任务要求同样渲染 AppName 短标签、title 放包名。
- 阻塞原因：`Overview.AppShare` 来自 store.go 的 `AppShareRow`（仅 App/Count/Pct 三字段），SQL 只 SELECT `m.app, COUNT(*)`，模板层拿不到 app_name。修复必须改 store.go（AppShareRow 加 AppName 字段 + SELECT 加 m.app_name + scan）和/或 web.go，超出白名单（其余只读）且违反「本修复不改 Go 逻辑，若发现必须改逻辑才能修，写 BLOCKED.md 停下」。
- 可选裁决：① 允许动 store.go 一行级改动（加字段+SELECT/scan），模板侧随即补一行完成；② 接受 App 占比保持包名原文（该列表为 flex 两列、无宽度挤压、无重叠 bug，仅显示一致性差异）。
- 已做：tail -5 与历史页均已改 AppName+title，仅 App 占比一项待裁决。
