你是执行者，这份文档是你唯一的任务来源；中途没人可问，拿不准的写进 `BLOCKED.md`（待裁决清单），跳过继续做别的，最后随交付一起提交。断了或换新会话：先读 `PROGRESS.md` 接着做，别重做。

这活为什么干：服务端（另一个 agent 并行在做）加了"设备必须绑定账号才能上报"，绑定要在网页上输入手机的 device_id 和 secret——所以手机设置页要把这两个值显示出来。干完世界什么样：用户打开 App 设置页就能看到并复制 device_id 和 secret，拿去网页绑定。其余功能一行不动。让步顺序：不动现有逻辑 > 显示正确。「只允许/不许」是死规矩，违反算失败；「建议」有更好的路就走，在 `PROGRESS.md` 记一句为什么。

**我替领导拍的板**
- 只在设置页（SettingsScreen）加一个"设备信息"区：设备 ID、设备密钥两行只读文本，数据来自现有 DeviceIdentity（不改它的生成逻辑），各带"复制"按钮（ClipboardManager）。
- 不改上报、采集、引导任何逻辑；不加新依赖；设置页其他内容不动。

**界限**
- 白名单：只允许改 `android/app/src/main/java/dev/sender/app/ui/SettingsScreen.kt`。其余文件只读，`server/`、`tasks/` 碰都不许碰。
- 现有 29 个测试一个不许改、不许删。

**现状与任务 0**
实测（2026-08-03）：`./gradlew testDebugUnitTest` 29 个全绿、0 skip；APK 可构建。任务 0：跑一次 `./gradlew testDebugUnitTest` 确认 29 绿，把「理解的目标/顺序/最大风险」≤10 行写进 `PROGRESS.md` 再动工。

**任务**
1. 设置页新增"设备信息"区：显示 `DeviceIdentity.deviceId` 与 `DeviceIdentity.secret`，各带复制按钮；页面其他内容不动。验收：`grep` 确认 SettingsScreen 里引用了 DeviceIdentity 的 deviceId 与 secret 并渲染出来。
2. 收尾：`./gradlew assembleDebug` + `./gradlew testDebugUnitTest` 全绿（仍 29 个、0 skip），贴输出。

**规矩**
- 防作弊点名：删/改测试、skip、`|| true` 全算失败；测试数只许 ≥29。
- 同一验收连败 3 次换下一项；结果比开工差就回滚如实报告——「没做成但说清了」合格，「做了但更糟」不合格。

**完成条件**
- 硬指标一：`./gradlew assembleDebug` 成功 + `testDebugUnitTest` 29 个全绿、0 skip，输出贴在对话里。
- 硬指标二：`SettingsScreen.kt` 中能 grep 到 `deviceId` 与 `secret` 的展示代码。
- 每条验收贴实际命令输出，只说做完了不算。`BLOCKED.md` 随交付提交，空的也写「无」。止损：或已跑满 4 轮——满轮即停，如实汇报卡在哪、还差什么。
