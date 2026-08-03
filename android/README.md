# Sender（安卓通知采集器）

采集 Android 通知（发送者+内容+时间）到本地 Room 库，按 App 分类查看/开关采集，并自动上报到自托管服务端。

## 构建与测试

```bash
./gradlew assembleDebug          # 产出 app/build/outputs/apk/debug/app-debug.apk
./gradlew testDebugUnitTest      # JVM 单测（无设备可跑）
```

环境要求：JDK 17+、Android SDK（compileSdk 35 / platform android-35，targetSdk 34，minSdk 26）。

## 安装

```bash
adb install app/build/outputs/apk/debug/app-debug.apk
```

打开 App 完成引导三步：

1. 开启通知使用权（跳系统设置 → 通知使用权 → 打开「通知采集」）
2. Android 13+ 通知权限（POST_NOTIFICATIONS）
3. 微信「我 → 设置 → 新消息通知 → 通知显示消息详情」打开（否则只能采到「你收到了一条消息」）

## 使用

- **已采集消息**：按 App 列表（图标、名称、今日条数、采集开关）。关闭开关后该 App 新通知在入库前被过滤。
- **消息列表**：点某 App 进入，按天分组显示时间/发送者/内容。
- **设置**：服务端地址（默认 `http://10.0.2.2:8080`，模拟器访问宿主机）、测试连接（打 `/healthz`）、「登录并绑定账号」一键 OAuth 绑定（浏览器授权后自动回跳，显示已绑定用户名）、设备 ID/注册状态。

## 上报契约（客户端侧）

- 注册：`POST /api/v1/devices/register`，header `X-Device-Secret: <32位hex>`，body `{"device_id":"<uuid>","name":"<机型>"}`；200 即成功，注册成功前不上传。
- 上报：`POST /api/v1/devices/{device_id}/messages`，header `Authorization: Bearer <secret>`，body `{"messages":[...]}`，单批 ≤500；2xx（含 `{"inserted":N,"duplicates":M}`）即成功，成功才标 `synced=1`。
- `client_msg_id = 包名:通知key:postTime毫秒`（通知更新带新 postTime → 新内容可采），本地唯一索引去重。
- 触发：新消息入库即触发（5 分钟最小间隔）+ 每 30 分钟兜底；失败指数退避重试；服务端幂等去重，重发无害。

## 已知限制

- **无前台服务**：通知监听由系统绑定（NotificationListenerService）。被「强行停止」后监听会失效，需重新打开 App（系统会重新绑定）。
- 绑定方式：设置页一键绑定（OAuth 授权码 + PKCE，`sender://oauth` 回跳；access_token 仅用于本次绑定，不落盘）。
- 微信「通知显示消息详情」无法程序化检测，引导页由用户手动确认。
