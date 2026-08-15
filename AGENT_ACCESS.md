# Agent 接入说明

本文档面向要读取 Sender 数据的 agent（Claude Code、自定义脚本等）。README 只留入口，具体接入方式都在这里。

## 最快路径：`sender` CLI（推荐）

CLI 把 OAuth 授权和查询 API 都包好了，agent 不需要手写 HTTP、不需要复制粘贴 code。

### 构建 / 获取

```sh
cd server && go build -o ../dist/sender ./cmd/sender
# 或直接用仓库 dist/ 里的现成二进制
```

### 授权登录（一次性）

```sh
./dist/sender login
```

CLI 会：
1. 在 `127.0.0.1` 上开一个本地回调端口；
2. 自动打开浏览器访问服务端的授权页（PKCE S256 + state）；
3. 用户登录后点「批准」→ 浏览器 302 回本地回调 → CLI 校验 state、换 token；
4. 把凭证存到 `~/.config/sender/config.json`（0600，7 天有效）。

**整个过程中用户不需要复制任何 code。** 无浏览器环境（SSH）用 `--no-browser`，CLI 只打印授权链接，用户在本机浏览器打开、点批准即可。

### 查询

```sh
sender messages --day 2026-08-12                       # 当天全部消息
sender messages --day 2026-08-12 --app com.tencent.mm  # 按 App 过滤
sender messages --day 2026-08-12 --device-id <id> --limit 500
sender messages --limit 50                             # 省略 --day = 最近 50 条
sender messages --cursor <ts:id>                       # 翻页（需配合 --day）
sender apps --day 2026-08-12                           # 按 App 聚合统计
sender status                                          # 登录状态 + 服务端健康
sender logout                                          # 删除本地凭证
```

默认输出 JSON（就是 API 原始响应，agent 可直接解析）；加 `--text` 输出人类可读表格。

### 环境变量

| 变量 | 作用 |
|---|---|
| `SENDER_SERVER` | 服务器地址（默认 `http://localhost:8080`） |
| `SENDER_TOKEN` | 直接提供 access token，跳过配置文件（无状态调用，适合每次单独跑命令的 agent） |
| `SENDER_CONFIG` | 配置文件路径（默认 `~/.config/sender/config.json`） |

无状态示例：`SENDER_TOKEN=<token> sender messages --server http://192.168.0.136:8080 --day 2026-08-12`

## 手动 HTTP 流程（不依赖 CLI）

适合把流程内嵌到自己实现里的 agent。

### 1. 生成 PKCE 对（S256）和 state

```sh
VERIFIER=$(openssl rand -base64 48 | tr -d '=' | tr '+/' '-_')
CHALLENGE=$(printf '%s' "$VERIFIER" | openssl dgst -sha256 -binary | openssl base64 -A | tr -d '=' | tr '+/' '-_')
STATE=$(openssl rand -hex 8)
```

### 2. 拼授权链接给用户

```sh
AUTHORIZE_URL="http://localhost:8080/authorize?response_type=code&client_id=my-agent&redirect_uri=$REDIRECT_URI&code_challenge=$CHALLENGE&code_challenge_method=S256&state=$STATE"
```

`client_id` 任意非空字符串即可，无需预注册。`redirect_uri` 二选一：

- **loopback（推荐）**：`http://127.0.0.1:<你的端口>/callback`。用户点「批准」后浏览器 302 回这个地址，`?code=…&state=…` 直接落到你本地监听的回调上，用户零操作；
- **OOB 兜底（无回调能力）**：`urn:ietf:wg:oauth:2.0:oob`。批准后页面显示一次性 code，用户复制回贴给你。

### 3. 换 access token

```sh
curl -sS -X POST http://localhost:8080/api/v1/oauth/token \
  -H 'Content-Type: application/json' \
  -d '{"grant_type":"authorization_code","code":"<code>","code_verifier":"<VERIFIER>","client_id":"my-agent","redirect_uri":"<REDIRECT_URI>"}'
# {"access_token":"...","token_type":"Bearer","expires_in":604800}
```

### 4. 查询数据

```sh
curl "http://localhost:8080/api/v1/messages?day=2026-08-12" \
  -H "Authorization: Bearer <access_token>"
curl "http://localhost:8080/api/v1/apps" \
  -H "Authorization: Bearer <access_token>"
```

## 权限与边界

- token 只能看到**绑定到你账号的设备**的数据（`/messages`、`/apps` 按账号过滤）。
- `limit` 默认 100、上限 500；分页用上一页返回的 `next_cursor`（格式 `ts:id`），**cursor 必须配合 `day` 过滤**，否则报错。
- 结果按 `ts` 升序，`id` 为并列时的确定性排序键。
- 采集层截断是固有限制：微信通知约几十字、X 约 280 字，服务端存的就是手机采到的原文，拿不到更长的版本。
- 授权码 5 分钟有效、单次使用；access token 7 天有效。

## 错误处理

- 所有错误统一 JSON：`{"error":"…"}`；未知路径 404。
- OAuth token 端点按 RFC 6749 报错：`invalid_request` / `invalid_grant`（code 未知、过期、复用，或 PKCE/redirect/client 不匹配）/ `server_error`。
- 未授权查询返回 401；设备未绑定返回 403。
