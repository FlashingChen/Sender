# direction-approved.md

## 项目：Sender Web UI 重设计

- 日期：2026-08-03
- 三方向初稿（真实 HTML + 截图）：
  - 方向 1 · Relay Console：`design/draft-1-relay-console.html` / `design/draft-1.png`
  - 方向 2 · Terminal：`design/draft-2-terminal.html` / `design/draft-2.png`（修正版）
  - 方向 3 · Pulse：`design/draft-3-pulse.html` / `design/draft-3.png`
- 用户选择原话（ask 工具选择）：**「方向 2 · Terminal」**（选项 label：方向 2 · Terminal；描述：全站等宽字体、日志流、琥珀色；个性最强）
- 用户修正原话（插话消息）：**「不能单独有"授权"那个页面，而是通过链接访问的，你看 Google 的 OAuth、Coded 的」** → 信息架构修正：授权同意页、OOB 码页、登录/注册页均为**无站点导航的独立页，仅通过链接直达**（/authorize、/login、/register、OOB）；应用内导航只有「概览 / 历史 / 绑定设备」。
- 生效范围：Sender Web UI 全部页面，以 `design/draft-2-terminal.html`（修正版）为视觉规格基准。
- 附带决策（同轮 ask 确认）：agent 授权码回传 = loopback 回调 + OOB 复制码兜底，两种都支持；历史页 = 列表+过滤 + 全文搜索 + 统计图表；设备码流程（RFC 8628）移除。
