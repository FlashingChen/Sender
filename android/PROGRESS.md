# PROGRESS

## 理解的目标/顺序/最大风险 (2026-08-03)

- 目标：安卓通知采集器 `dev.sender.app`：NLS 捕获通知 → 开关过滤 → Room 去重入库 → 按 App 查看/开关 → WorkManager 上报自托管服务端（注册成功前不上传，2xx 才标 synced=1，单批 ≤500）。
- 顺序：环境核对 → 工程骨架(assembleDebug) → 采集链路 → 管理 UI → 上报链路 → 引导/设置 → 单测(≥12) → README。
- 契约要点：`client_msg_id=包名:通知key:postTime毫秒`；body 只许契约字段；`Authorization: Bearer <secret>`；`X-Device-Secret` 走注册；`ts` Unix 秒；2xx 即成功。
- 最大风险：①版本兼容（Gradle/AGP/Kotlin/Robolectric 组合），已选保守组合 Gradle 8.13 + AGP 8.7.3 + Kotlin 2.0.21；②Room DAO 无法在纯 JVM 上实例化，去重/开关过滤必须真库真 SQLite 验证 → 引入 Robolectric（测试专用，见 BLOCKED.md）；③构建/测试依赖外网下载。
- 改动决策记录：JSON payload 用手写序列化（固定形状、可控无多余字段），不引 JSON 库依赖；响应只判 2xx 状态码，无需解析。
- 只管 `android/`，不碰 `server/`；服务端契约以服务端 agent 的书为准。

## 进度日志

- [x] 任务0：环境核对。Java 21.0.11 ✓（≥17）；gradle 9.6.1（本机，wrapper 需生成）；ANDROID_HOME=/Users/flashingchen/android-sdk ✓；platforms 含 android-34 ✓；build-tools 仅 28.0.3/35.0.0（无 34.x，见 BLOCKED）；licenses 已接受；网络通；adb 无真机。
- [x] 任务0：生成 gradle wrapper 8.13（用本机 gradle 生成）。
- [x] 任务1：工程骨架完成，`./gradlew assembleDebug` 产出 APK（见对话输出）。过程修正：compileSdk 34→35（WorkManager 2.10 强制要求，见 BLOCKED）；Room 聚合查询列需显式 CAST(… AS INTEGER)；列名按属性名（clientMsgId 用 @ColumnInfo 映射 client_msg_id）。
- [x] 任务2：采集链路（NLS→开关过滤→去重入库）完成。去重/过滤在真 Room+SQLite 上有单测（Robolectric）。
- [x] 任务3：管理 UI 两界面（App 列表 / 消息列表按天分组）完成，数据全来自本地库 Flow。
- [x] 任务4：上报链路完成。修正：SyncEngine 需记录 uploadedAny（否则成功收尾返回 NOTHING_PENDING）；ApiClient 连接工厂调用移入 try（网络异常不外泄）。
- [x] 任务5：引导页三步 + 设置页（服务端地址 + 测试连接 /healthz）完成。
- [x] 任务6：单测全绿 29 个（≥12），`./gradlew testDebugUnitTest` 输出见对话。
- [x] README.md（含已知限制：无前台服务，被强行停止后需重开 App）。
- [x] 收尾：`assembleDebug` + `testDebugUnitTest` 最终复跑通过。

## Phase 2（2026-08-03）：设置页加设备信息区

- 目标：设置页新增「设备信息」区，只读展示 `DeviceIdentity.deviceId` / `secret`，各带复制按钮（ClipboardManager），供用户在网页绑定账号；其余功能一行不动。
- 顺序：基线 29 绿确认 → 仅改 SettingsScreen.kt → assembleDebug + testDebugUnitTest 复跑，贴输出。
- 最大风险：①误改白名单外文件；②新 UI 引入编译错误——改动收敛在单文件，双命令复跑兜底。
- 决策：保留现有「设备 ID：」展示行不删（死规矩：设置页其他内容不动），新区另起展示；复制走系统 ClipboardManager（零新依赖）；复制后按钮短暂显示「已复制」。
- [x] Phase 2 完成：设置页新增「设备信息」区（设备 ID / 设备密钥 + 复制按钮），`assembleDebug` 成功，`testDebugUnitTest` 29 全绿 0 skip（输出见对话）。

## Phase 3（2026-08-03）：OAuth 一键绑定账号

- 目标：设置页点「登录并绑定账号」→ 浏览器 OAuth 授权 → `sender://oauth` 回调 → state 校验 → 换 token → bind 设备 → 存用户名；上报仍走设备 secret，零改动。
- 顺序：基线 29 绿（已确认）→ `net/OAuth.kt`（PKCE/授权 URL/token 交换/bind）→ 单测 ≥4 → SettingsStore 加 boundUsername → MainActivity 深链（singleTask + onNewIntent/onResume）→ 设置页按钮与状态 → manifest → README → 双命令复跑。
- 最大风险：①state/verifier 跨 Activity 生命周期保持 → 放进程级 `OAuthSession` 对象（旋转不丢）；②OAuth 需读响应体解析 token/username，ApiClient 只判状态码 → OAuth 自建连接（复用其 HttpURLConnection 模式）；③白名单只许 7 个文件，越界即失败。
- 决策：token 交换/绑定锁定发起时的服务端地址（存 OAuthSession），不随设置实时变；回调 state 不符直接拒绝且保留待定会话；`error=access_denied` 显示「已取消授权」。
- [x] Phase 3 完成：`net/OAuth.kt`（PkceGenerator/OAuthSession/OAuthCallback/OAuth）+ 设置页按钮与状态 + MainActivity 深链（singleTask、onNewIntent/onResume、state 校验、换 token、bind、存 boundUsername）+ manifest intent-filter + OAuthTest 11 个。修正：bind 响应 `ok` 是 JSON 布尔（非字符串），需按布尔解析。`assembleDebug` 成功，`testDebugUnitTest` 40 全绿 0 skip（输出见对话）。README 已知限制已补「绑定方式：设置页一键绑定」。
