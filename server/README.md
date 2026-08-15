# Sender Server

Self-hosted notification ingestion and query service. It uses Go's `net/http`,
SQLite through `modernc.org/sqlite` (no CGO, FTS5 included), and an embedded
terminal-style Web UI (monospace, amber accent, `$` prompt). Accounts use
bcrypt passwords; OAuth authorization-code + PKCE issues access tokens for
query clients.

## Run locally

Requirements: Go 1.26.4 or newer (see `go.mod`).

```sh
cd server
go run ./cmd/server
```

The server listens on `:8080` by default. Local SQLite data is stored at
`data/messages.db`. Override the address and database path with `ADDR` and
`DB_PATH`:

```sh
ADDR=:9090 DB_PATH=/tmp/sender.db TZ=UTC ALLOW_REGISTRATION=true go run ./cmd/server
```

`TZ` controls the `day` stored for each Unix-second `ts`. The default is
`Asia/Shanghai`. It must be an installed IANA time-zone name.

`ALLOW_REGISTRATION` defaults to `true` when unset (local runs), but the
Docker image ships with `ALLOW_REGISTRATION=false`. Device registration only
happens once per device, so a recommended flow is: start with
`ALLOW_REGISTRATION=true`, register your account and all devices, then set it
to `false` and restart to close both the Web and device-registration
interfaces.

Registration is rate limited (10 attempts per IP per 5 minutes), and an
existing device's secret is never rotated: re-registering the same
`device_id` with the same secret is idempotent (only the display name
updates), while a conflicting secret returns `409`.

Health check:

```sh
curl http://localhost:8080/healthz
# {"ok":true}
```

## TLS

The server itself speaks plain HTTP by default. Two supported options:

1. **Reverse proxy (recommended)**: terminate TLS at nginx/Caddy/cloudflare
   and forward to the server. The session cookie automatically gets the
   `Secure` flag when the request arrives with `X-Forwarded-Proto: https`.
2. **Direct TLS**: set `TLS_CERT` and `TLS_KEY` to PEM file paths and the
   server serves HTTPS itself:

   ```sh
   TLS_CERT=/etc/letsencrypt/live/example.com/fullchain.pem \
   TLS_KEY=/etc/letsencrypt/live/example.com/privkey.pem \
   go run ./cmd/server
   ```

Exposing the plaintext port to an untrusted network leaks the session cookie,
device secrets, and all captured message content; prefer TLS whenever the
server is reachable beyond localhost.

## Docker Compose

The Compose setup builds the multi-stage `Dockerfile`, publishes port 8080, and
persists SQLite under the named `sender-data` volume:

```sh
docker compose up -d --build
curl http://localhost:8080/healthz
docker compose down
```

The service container uses `DB_PATH=/data/messages.db` and
`TZ=Asia/Shanghai`. Change those environment values in `docker-compose.yml` if
needed. `docker compose down -v` removes the database volume and its data.

## Web UI

The app pages share a top navigation (概览 / 历史 / 绑定设备) and require
login:

- `/overview`: KPI cards (today, rolling week, authorized agents), bound-device
  count, uplink health, a seven-day trend chart, `tail -5` of the latest
  messages, and the app share breakdown. All numbers are SQL aggregates
  rendered server-side.
- `/history`: date / app / device filters, FTS5 full-text grep across
  content, sender, and chat, and OFFSET pagination at 50 rows per page with
  the total match count.
- `/bind`: bind a phone by entering its `device_id` and device secret, and
  list already-bound devices.
- `/logout`: clear the seven-day HttpOnly, SameSite=Lax session cookie.

Standalone pages (no site navigation, reached by direct link) are
`/login`, `/register`, `/authorize` (the sudo-style consent page), and the
OOB one-time-code page rendered after approving an OOB authorization.

## OAuth authorization-code + PKCE

Any non-empty `client_id` is accepted; no client pre-registration is needed.
PKCE S256 is mandatory. `GET /authorize` validates the request, sends
unauthenticated users to `/login?next=…`, and shows the sudo-style consent
page. Approving redirects the browser back to `redirect_uri` with
`code` + `state`; rejecting redirects with `error=access_denied` + `state`.

`redirect_uri` must be one of: `http://localhost[:port]/*` or
`http://127.0.0.1[:port]/*` (loopback), any `https://*`, any non-http custom
scheme (mobile apps may use `sender://`), or the OOB fallback
`urn:ietf:wg:oauth:2.0:oob` which renders the code on a page instead of
redirecting. Everything else is rejected. Scheme-capable execution vectors
(`javascript:`, `data:`, `vbscript:`, `file:`, `about:`, `blob:`) are always
rejected even though they are non-http.

Authorization codes are 32 hex characters, expire after 300 seconds, are
single-use, and are stored hashed (SHA-256) and bound to
`client_id` / `redirect_uri` / `code_challenge` / `user_id`.

### Agent integration (authorization link)

1. Build the authorization URL with a fresh PKCE pair:

   ```sh
   VERIFIER=$(openssl rand -base64 48 | tr -d '=' | tr '+/' '-_')
   CHALLENGE=$(printf '%s' "$VERIFIER" | openssl dgst -sha256 -binary | openssl base64 -A | tr -d '=' | tr '+/' '-_')
   STATE=$(openssl rand -hex 8)

   AUTHORIZE_URL="http://localhost:8080/authorize?response_type=code&client_id=my-agent&redirect_uri=http://localhost:9999/callback&code_challenge=$CHALLENGE&code_challenge_method=S256&state=$STATE"
   echo "$AUTHORIZE_URL"
   ```

2. Show the link to the user. They log in and click 批准 on the sudo-style
   consent page.

3. Exchange the code for an access token:

   ```sh
   CODE=… # from the redirect: /callback?code=…&state=…
   curl -sS -X POST http://localhost:8080/api/v1/oauth/token \
     -H 'Content-Type: application/json' \
     -d '{"grant_type":"authorization_code","code":"'"$CODE"'",
          "code_verifier":"'"$VERIFIER"'","client_id":"my-agent",
          "redirect_uri":"http://localhost:9999/callback"}'
   # {"access_token":"...","token_type":"Bearer","expires_in":604800}
   ```

   Errors follow RFC 6749: `invalid_request`, `invalid_grant` (unknown,
   expired, or reused code; PKCE, redirect, or client mismatch),
   `invalid_client`, `server_error`.

### Sender CLI (recommended for agents)

`cmd/sender` is the agent-facing CLI: it runs the OAuth flow itself and wraps
the query API, so agents never handle HTTP or paste codes. Login uses the
RFC 8252 loopback flow — the CLI binds a `127.0.0.1` listener, opens the
browser, and the user's single click on 批准 completes the authorization
(Google/gcloud style). No code is ever shown or copied.

Build and install:

```sh
cd server
go build -o dist/sender ./cmd/sender
```

Commands:

```sh
sender login                  # 浏览器点「批准」即完成，无需复制 code
sender login --no-browser     # 只打印授权链接（SSH/无浏览器环境）
sender status                 # 登录状态、token 过期时间、服务端健康
sender messages --day 2026-08-12
sender messages --day 2026-08-12 --app com.tencent.mm --limit 500
sender apps --day 2026-08-12
sender logout                 # 删除本地凭证
```

Output is JSON by default (the raw API payload, ready for agent
consumption); add `--text` for a human-readable table. Environment
overrides: `SENDER_SERVER`, `SENDER_TOKEN` (stateless agent calls without a
config file), `SENDER_CONFIG` (default `~/.config/sender/config.json`,
written 0600). See `sender help` for the full flag list.

### OOB fallback (no callback receiver)

Use `redirect_uri=urn:ietf:wg:oauth:2.0:oob`. After approval the server
renders the one-time code on a standalone page; the user copies it back to
the agent, which then calls the same token endpoint with
`"redirect_uri":"urn:ietf:wg:oauth:2.0:oob"`.

## Device registration, binding, and upload

All API routes below use the `/api/v1` prefix. A device creates its own UUID
and a 32-character hexadecimal secret. Registration still uses the device
secret:

```sh
DEVICE_ID=550e8400-e29b-41d4-a716-446655440000
SECRET=0123456789abcdef0123456789abcdef

curl -i -X POST http://localhost:8080/api/v1/devices/register \
  -H "X-Device-Secret: $SECRET" \
  -H 'Content-Type: application/json' \
  -d '{"device_id":"'"$DEVICE_ID"'","name":"Pixel 8"}'
# HTTP/1.1 200 OK
# {"ok":true}
```

Bind the registered device with either a logged-in session cookie or a Bearer
access token (the Android app binds with its access token). A device can only
be bound to one account: re-binding a device already owned by another account
returns `409`, while re-binding to the same account is idempotent:

```sh
curl -i -X POST http://localhost:8080/api/v1/devices/bind \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <access-token>' \
  -d '{"device_id":"'"$DEVICE_ID"'","secret":"'"$SECRET"'"}'
# {"ok":true,"username":"alice"}
```

Upload up to 500 messages per request. `client_msg_id` is globally unique;
repeated IDs are not inserted and are counted as `duplicates`. Upload keeps
using the device secret, but the device must already be bound:

```sh
curl -i -X POST "http://localhost:8080/api/v1/devices/$DEVICE_ID/messages" \
  -H "Authorization: Bearer $SECRET" \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"client_msg_id":"com.tencent.mm:notif_key:1780000000123","app":"com.tencent.mm","app_name":"微信","chat":"张三","sender":"张三","content":"今晚吃饭吗","ts":1780000000}]}'
# {"inserted":1,"duplicates":0}
```

An unbound device receives `403 {"error":"device not bound"}`. A bad or
missing device-secret Bearer credential remains `401`.

## Query API

`GET /api/v1/messages` and `GET /api/v1/apps` require the OAuth access token:

```sh
ACCESS_TOKEN=...
DAY=2026-08-03
curl "http://localhost:8080/api/v1/messages?day=$DAY&device_id=$DEVICE_ID&limit=100" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
curl "http://localhost:8080/api/v1/apps?day=$DAY&device_id=$DEVICE_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

The token only returns messages from devices bound to its account. `limit`
defaults to 100 and is capped at 500. For day-scoped pagination, `cursor` is
the previous page's last composite `ts:id` key, for example `1785690100:8`;
a cursor without a `day` filter is rejected. Numeric-only cursors are
invalid. Results are ordered by `ts` ascending with `id` as the
deterministic tie-breaker. Omit `day` to get the most recent `limit`
messages; optional `device_id` and `app` filters work with either form.

## 给 agent 的接入说明

agent 接入（CLI 用法 + 手动 OAuth 流程 + 权限边界 + 错误处理）统一维护在
仓库根目录的 [`AGENT_ACCESS.md`](../AGENT_ACCESS.md)，README 不再重复。

Invalid JSON and invalid request parameters return a JSON error object such
as `{"error":"invalid JSON"}`. Unknown paths return 404. Production databases
are migrated additively; existing devices and messages are retained, and the
legacy device-grant table is dropped on migration.

## Known limitations

- 通知预览长度限制：微信约几十字、X 约 280 字，超长消息在采集时就被截断，服务端存的就是手机采到的原文；需完整文案可考虑无障碍方案（未做）。
