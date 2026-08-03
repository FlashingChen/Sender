你是执行者，这份文档是你唯一的任务来源；中途没人可问，拿不准的写进 `BLOCKED.md`（待裁决清单），跳过继续做别的，最后随交付一起提交。断了或换新会话：先读 `PROGRESS.md` 接着做，别重做。

这活为什么干：让手机 App 能一键完成"账号登录 + 设备绑定"，不用再手动复制粘贴 device\_id/secret。干完世界什么样：用户在设置页点「登录并绑定账号」，手机浏览器弹出 OAuth 授权页（服务端另一个 agent 并行在做），登录并批准后自动跳回 App，设备绑定完成，设置页显示「已绑定：用户名」。让步顺序：不动现有采集/上报逻辑 > 流程正确 > 状态提示友好。「只允许/不许」是死规矩，违反算失败；「建议」有更好的路就走，在 `PROGRESS.md` 记一句为什么。

**契约（与服务端书同一版本，逐字对齐；服务端是授权流程实现方，有出入以服务端书为准）**

* 授权 URL：`GET {server}/authorize?response_type=code&client_id=sender-android&redirect_uri=sender%3A%2F%2Foauth&code_challenge=<S256挑战>&code_challenge_method=S256&state=<随机>`；{server} = 设置页已填的服务端地址。

* 回调：浏览器重定向到 `sender://oauth?code=…&state=…`（或 `error=access_denied&state=…`）。

* 换 token：`POST {server}/api/v1/oauth/token`，表单：`grant_type=authorization_code&code=…&code_verifier=…&client_id=sender-android&redirect_uri=sender://oauth`，成功返回 `{"access_token":"…","token_type":"Bearer","expires_in":…}`。

* 绑定：`POST {server}/api/v1/devices/bind`，`Authorization: Bearer <access_token>`，body `{"device_id":"…","secret":"…"}`，成功返回 `{"ok":true,"username":"…"}`。device\_id/secret 用现有 DeviceIdentity，不改生成逻辑。

* PKCE：code\_verifier 43 字符随机（A-Za-z0-9-.\_），code\_challenge = base64url(sha256(verifier)) 去 padding。

**我替领导拍的板**

* 新文件 `net/OAuth.kt`：PkceGenerator（verifier/challenge/state 生成）、授权 URL 构建、token 交换请求、bind 请求；HTTP 复用 ApiClient 的 HttpURLConnection 模式。

* 设置页（SettingsScreen.kt）：服务端地址下方加「登录并绑定账号」按钮（服务端地址为空时禁用）；已绑定显示「已绑定：用户名」+ 可再次绑定；失败/取消显示提示。

* MainActivity：`launchMode="singleTask"` + intent-filter（`android:scheme="sender"`、`android:host="oauth"`）；处理 `onNewIntent` 与 `onResume` 两种入口；校验 state 与发起时一致再换 token；成功后保存用户名（SettingsStore 加字段），刷新设置页。

* 换到的 access\_token 仅用于绑定这一次，不落盘。

* 绑定成功后不阻塞上报：上报仍走设备 secret，逻辑零改动。

**界限**

* 白名单：只允许改/建 `android/app/src/main/java/dev/sender/app/ui/SettingsScreen.kt`、`ui/MainActivity.kt`、`net/OAuth.kt`（新建）、`settings/SettingsStore.kt`、`AndroidManifest.xml`、`app/src/test/java/dev/sender/app/net/OAuthTest.kt`（新建）、`README.md`。其余只读，`server/`、`tasks/`、`design/` 碰都不许碰。

* 现有 29 个测试一个不许改、不许删。

**现状与任务 0**
实测（2026-08-03）：`./gradlew testDebugUnitTest` 29 个全绿、0 skip；APK 可构建。任务 0：跑一次确认 29 绿，把「理解的目标/顺序/最大风险」≤10 行写进 `PROGRESS.md` 再动工。

**任务**

1. `net/OAuth.kt`：PKCE 生成（verifier 43 字符、challenge 正确编码）、授权 URL 构建（参数齐全且编码正确）、token 交换与 bind 请求（成功/失败返回）。验收：单测覆盖。
2. MainActivity 深链：singleTask + intent-filter + onNewIntent/onResume 解析（code/error/state）→ state 校验 → 换 token → bind → 保存用户名 → 界面提示；`error=access_denied` 提示「已取消授权」。
3. 设置页按钮与状态：按钮、已绑定展示、地址为空禁用、操作中/成功/失败状态。
4. 测试（新增 ≥4，总数 ≥33，0 skip）：固定 verifier 断言 challenge 值、授权 URL 参数与编码、state 校验（不符拒绝）、回调解析（code 与 error 分支）。
5. 收尾：`./gradlew assembleDebug` + `testDebugUnitTest` 全绿 ≥33、0 skip，贴输出；README 已知限制补充「绑定方式：设置页一键绑定」。

**规矩**

* 防作弊点名：删/改测试、skip、放宽断言、mock 被测对象、`|| true` 全算失败；测试数只许 ≥33。

* 同一验收连败 3 次换下一项；结果比开工差就回滚如实报告——「没做成但说清了」合格，「做了但更糟」不合格。

**完成条件**

* 硬指标一：`./gradlew assembleDebug` 成功 + `testDebugUnitTest` 全绿 ≥33、0 skip，输出贴在对话里。

* 硬指标二：`AndroidManifest.xml` 可 grep 到 `sender` scheme 的 intent-filter；`SettingsScreen.kt` 可 grep 到「登录并绑定账号」按钮与已绑定用户名展示；`net/OAuth.kt` 存在。

* 每条验收贴实际命令输出，只说做完了不算。`BLOCKED.md` 随交付提交，空的也写「无」。止损：或已跑满 4 轮——满轮即停，如实汇报卡在哪、还差什么。

