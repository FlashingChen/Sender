# Sender 部署教程

自托管个人通知采集系统：安卓端「通知采集」App 采集通知（发送者 + 内容 + 时间）→ 自托管 Go 服务端存储 → Web 控制台与 OAuth API 查询。

```
安卓端「通知采集」 ──上报(设备密钥鉴权)──> sender-server(Go+SQLite) ──> Web 控制台 / OAuth API
```

本教程覆盖完整落地：服务端部署 → 安卓端安装 → 权限配置 → 设备绑定 → 端到端验证 → 对外暴露与备份。全程约 15 分钟。

---

## 0. 前置要求

| 项 | 要求 |
|---|---|
| 服务端主机 | 一台常开的电脑/服务器（Mac / Linux），能访问互联网（设备上报用）；局域网部署需与手机同一网络 |
| 安卓手机 | Android 8.0+（minSdk 26），建议 Android 13+ |
| 构建工具（仅自建时） | JDK 17+、Android SDK（compileSdk 35）、`adb` |

---

## 1. 部署服务端（Linux 服务器，一步步来）

> 以下命令以 `/www/wwwroot/Sender` 为部署目录（宝塔常见路径），可自选；`<服务器IP>` 换成你的公网 IP。每步做完再做下一步。

### 第 1 步：上传二进制

在你电脑上（Sender 仓库根目录）执行，把 Linux 版二进制传上去，并建好数据目录：

```sh
scp dist/sender-server-linux-amd64 root@<服务器IP>:/www/wwwroot/Sender/
ssh root@<服务器IP> "mkdir -p /www/wwwroot/Sender/data"
```

> 宝塔用户也可以直接打开面板「文件」管理器，上传到 `/www/wwwroot/Sender/`，再新建 `data` 目录。后续命令都通过面板「终端」或 SSH 在服务器上执行。

### 第 2 步：试跑验证

```sh
cd /www/wwwroot/Sender && chmod +x sender-server-linux-amd64
./sender-server-linux-amd64
```

另开一个终端（或面板终端新标签）：

```sh
curl http://127.0.0.1:8080/healthz     # 期望 {"ok":true}
```

看到 `{"ok":true}` 后，回第一个终端按 `Ctrl+C` 停掉。试跑只为确认二进制没问题，正式运行交给第 3 步的 systemd 守护。

### 第 3 步（注册前）：配置 systemd 守护并启动

```sh
sudo tee /etc/systemd/system/sender-server.service > /dev/null <<'EOF'
[Unit]
Description=Sender notification relay server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/www/wwwroot/Sender
ExecStart=/www/wwwroot/Sender/sender-server-linux-amd64
Environment=ADDR=:8080
Environment=DB_PATH=/www/wwwroot/Sender/data/messages.db
Environment=TZ=Asia/Shanghai
Environment=ALLOW_REGISTRATION=true
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now sender-server
systemctl status sender-server          # 期望 active (running)
curl http://127.0.0.1:8080/healthz      # 期望 {"ok":true}
```

注意：此刻 `ALLOW_REGISTRATION=true` 是**故意的**——注册窗口只在注册前后打开，第 6 步会关掉。

### 第 4 步（注册前）：放行 8080 端口

浏览器要能访问到注册页，需放行端口（三选一，都要做就都做）：

- **宝塔面板**：安全 → 放行端口 → 添加 `8080`（TCP）
- **云厂商安全组**：入方向规则放行 TCP `8080`
- **命令行**（如 firewalld）：
  ```sh
  sudo firewall-cmd --permanent --add-port=8080/tcp && sudo firewall-cmd --reload
  ```

### 第 5 步：注册账号

浏览器打开 `http://<服务器IP>:8080/register`，填用户名 + 密码注册。注册完登录 Web 控制台，确认能进。

### 第 6 步（注册后）：关闭注册

**必须做**。把 unit 里的 `ALLOW_REGISTRATION` 改成 `false` 并重启：

```sh
sudo sed -i 's/ALLOW_REGISTRATION=true/ALLOW_REGISTRATION=false/' /etc/systemd/system/sender-server.service
sudo systemctl daemon-reload && sudo systemctl restart sender-server
```

验证已关闭（实测：注册关闭后 `/register` 返回 403，登录不受影响）：

```sh
curl -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/register    # 期望 403
curl http://127.0.0.1:8080/healthz                                      # 期望 {"ok":true}
```

### 第 7 步：日常维护

```sh
systemctl status sender-server     # 看状态
journalctl -u sender-server -f     # 看实时日志
systemctl restart sender-server    # 改配置后重启
```

`Restart=always` 保证崩溃自动拉起，`enable` 保证开机自启——之后不用再管它。

### 其他方式 A：macOS 本机部署（launchd 守护）

本机（Mac）跑二进制时用 launchd 托管：登录自启 + 崩溃自动重启。以用户级 LaunchAgent 为例（无需 sudo）：

```sh
mkdir -p ~/Library/LaunchAgents
cat > ~/Library/LaunchAgents/com.sender.server.plist <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.sender.server</string>
    <key>ProgramArguments</key>
    <array>
        <string>/绝对/路径/到/Sender/dist/sender-server</string>
    </array>
    <key>WorkingDirectory</key>
    <string>/绝对/路径/到/Sender</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>ADDR</key><string>:8080</string>
        <key>DB_PATH</key><string>data/messages.db</string>
        <key>TZ</key><string>Asia/Shanghai</string>
        <key>ALLOW_REGISTRATION</key><string>false</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/sender-server.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/sender-server.err.log</string>
</dict>
</plist>
EOF
plutil -lint ~/Library/LaunchAgents/com.sender.server.plist   # 期望 OK
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.sender.server.plist
```

常用命令：`launchctl bootout gui/$(id -u)/com.sender.server`（停止卸载）、`launchctl print gui/$(id -u)/com.sender.server`（查看状态）、`tail -f /tmp/sender-server.err.log`（看日志）。改 plist 后需 `bootout` + `bootstrap` 重载。注册账号流程同 Linux：先 `ALLOW_REGISTRATION=true` 启动注册，再改 `false` 重载。

### 其他方式 B：Docker 部署

```sh
cd server
docker compose up -d --build
```

- 持久化在 named volume `sender-data`，自带 `/healthz` healthcheck，`restart: unless-stopped` 保证崩溃自动拉起
- 镜像已默认 `ALLOW_REGISTRATION=false`：**Docker 方式先注册不了**，需要先临时放开：在 `docker-compose.yml` 的 `environment` 加 `ALLOW_REGISTRATION: "true"` → `docker compose up -d` 注册账号 → 删掉该行 → 再 `docker compose up -d`
- 注册流程（第 4~6 步）同样适用，改法换成改 compose 文件

---

## 2. 安装安卓端

### 2.1 构建 APK

```sh
cd android
./gradlew assembleDebug        # 产物：app/build/outputs/apk/debug/app-debug.apk
```

不想自己构建可直接用仓库里的 `dist/Sender-debug.apk`。包名 `dev.sender.app`，App 显示名「通知采集」。

### 2.2 安装到手机

**方式 A：adb（需开启 USB 调试）**

```sh
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

- **小米/红米（HyperOS）特别注意**：需要在手机「设置 → 更多设置 → 开发者选项 → USB 安装」打开，否则报 `INSTALL_FAILED_USER_RESTRICTED: Install canceled by user`；该开关可能要求登录小米账号 + 插入 SIM 卡才能打开。
- 安装时手机屏幕可能弹出确认框，需点「允许/安装」。

**方式 B：直接传 APK 安装**

把 `Sender-debug.apk` 通过微信/网盘/数据线拷到手机，用文件管理器打开安装（需允许「安装未知应用」）。

### 2.3 权限配置（关键，共三步）

打开 App「通知采集」，依次完成：

1. **通知使用权**：设置页点「开启通知使用权」跳系统设置（或手动：设置 → 通知与控制中心 → 通知使用权）→ 打开「通知采集服务」。这是采集的根基，不开就采不到任何通知。
2. **通知权限**（Android 13+）：允许「通知采集」发送通知，App 才能收到系统转发。
3. **微信显示详情**（采微信内容必需）：微信「我 → 设置 → 新消息通知 → 通知显示消息详情」打开，否则微信只报「你收到了一条消息」。

**小米/红米（HyperOS）额外建议**（不设置可能被杀后台、漏采）：

- 设置 → 应用设置 → 应用管理 → 通知采集 → **自启动**：打开
- 同一页面 → **省电策略**：选「无限制」
- 最近任务列表里把 App 下拉锁定（加锁图标）

> 已知限制：App 无前台服务（通知监听由系统绑定）。被「强行停止」后监听失效，重新打开一次 App 即可恢复绑定。

---

## 3. 联通与绑定

### 3.1 填服务端地址

App「设置」页 → 服务端地址：

- 真机连同一局域网：填 `http://<服务端主机局域网IP>:8080`（例：`http://192.168.0.136:8080`；IP 用 `ipconfig getifaddr en0` / `ip addr` 查）
- 模拟器：`http://10.0.2.2:8080`（模拟器访问宿主机的专用地址，默认值）
- App 内对非回环 `http://` 地址会显示明文警告，局域网内可忽略；公网请走 HTTPS（见第 5 节）

点「测试连接」应提示成功（打 `/healthz`）。

### 3.2 绑定账号（两种方式任选）

- **App 内一键绑定**：设置页「登录并绑定账号」→ 浏览器打开服务端登录/授权页 → 授权后自动回跳 `sender://oauth` 完成绑定（OAuth 授权码 + PKCE，access_token 不落盘）。
- **网页手动绑定**：登录 Web 控制台 →「绑定设备」页 → 粘贴 App 设置页显示的「设备 ID + 设备密钥」。

> 设备必须先注册（App 首次启动自动注册）再绑定账号，否则上报被拒（403）。未绑定设备的密钥不可通过注册接口轮换（409）。

---

## 4. 端到端验证

1. 手机任意 App 收一条新通知（如微信/短信），等 App 收到并入库（实时）。
2. 服务端日志应出现上报记录（`journalctl -u sender-server -f`）；若有失败会指数退避重试。
3. 浏览器登录 Web 控制台：概览页应看到今日条数、App 占比出现微信/X 等；历史页能看到消息原文。
4. API 抽查（agent 视角，需绑定设备的 token）：
   ```sh
   curl -H "Authorization: Bearer <token>" "http://<服务器>:8080/api/v1/messages?day=2026-08-04"
   ```

---

## 5. 对外暴露（可选，建议 HTTPS）

明文 HTTP 仅限局域网。要公网访问，置于 HTTPS 反向代理后（Caddy/Nginx）：

- 服务端已支持 `X-Forwarded-Proto`：经 HTTPS 反代时会话 cookie 自动带 `Secure`。
- 反代配置要点：`proxy_pass http://127.0.0.1:8080` + 转发 `X-Forwarded-*` 头。
- 安卓端地址改为 `https://<你的域名>`，App 内的明文警告随之消失。

---

## 6. 备份

数据全在 SQLite 单文件：备份 = 复制 `data/messages.db`（停服拷贝最稳妥，或直接 cp 也可，SQLite 支持热拷贝）。建议定时任务每天拷一份。

```sh
cp data/messages.db data/backup-$(date +%F).db
```

---

## 7. 常见问题排查

| 现象 | 原因与处理 |
|---|---|
| 装了收不到任何通知 | 通知使用权没开，或 App 被强行停止过（重新打开一次 App）；小米机检查自启动/省电策略 |
| 测试连接失败 | 地址不是同一网络；服务端没起（`curl /healthz` 自测）；路由器 AP 隔离 |
| 上报 403 | 设备未绑定账号（见 3.2） |
| 微信只有「你收到了一条消息」 | 微信「通知显示消息详情」未开（见 2.3） |
| 注册页打不开/注册失败 | `ALLOW_REGISTRATION=false` 已关闭注册（见 §1 第 6 步） |
| 内容被截断 | 采集层固有限制：微信约几十字、X 约 280 字，服务端存的就是手机采到的原文 |
| 重复消息 | 不会：`client_msg_id = 包名:通知key:postTime毫秒` 幂等去重，重发无害 |

---

## 8. 安全基线（务必执行）

- 注册完账号 → `ALLOW_REGISTRATION=false`（见 §1 第 6 步）。
- 设备密钥用 AndroidKeyStore AES-GCM 加密存手机（`allowBackup=false`，备份不进云）。
- 数据敏感（含微信消息原文）：公网必须 HTTPS；三层写侧防护（设备密钥 → 设备绑定 → 关闭注册）是唯一防线，别开裸公网端口。
- 登录/注册/换 token 已按 IP 限流（10 次/5 分钟）。
