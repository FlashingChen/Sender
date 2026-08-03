# BLOCKED（待裁决清单）

1. **build-tools 34.x 缺失**（SDK 只有 28.0.3 / 35.0.0）：处理 = `buildToolsVersion = "35.0.0"` 显式指定已装版本，非新增依赖，不阻塞。若裁决要求严格 34.x，需 sdkmanager 下载（网络可达）。
2. **单测引入 Robolectric**（`testImplementation`，测试专用依赖）：去重（同 client_msg_id 只存一条）与开关过滤（关了的包插入前被挡）必须在真实 Room + 真实 SQLite 上验证，纯 JVM 无法实例化 Room。Robolectric 是 JVM 上跑 Android 框架的标准方案，无设备可跑，属「必须加的测试依赖」，记此待裁决。
3. **无真机/模拟器**（`adb devices` 为空）：装 APK 真机冒烟做不了；硬指标（assembleDebug + testDebugUnitTest）不受影响。README 已写安装步骤。
4. **compileSdk 提到 35**：WorkManager 2.10.0 强制要求 compileSdk ≥35（依赖检查报错）。platform android-35 已装；targetSdk 仍按领导拍板保持 34（compileSdk 与 targetSdk 相互独立，官方说明如此）。记此待裁决。
5. 其他待裁决：无。

## Phase 2 新增待裁决（2026-08-03）

无。设置页设备信息区按领导拍板完成，无阻塞项。

## Phase 3 新增待裁决（2026-08-03）

无。OAuth 一键绑定按契约书完成，无阻塞项。
