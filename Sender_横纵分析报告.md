# Sender：把手机消息变成数据管道的自托管工具

> 研究时间：2026-08-03 | 所属领域：移动工具 / 自托管基础设施 / AI Agent 数据管道 | 研究对象类型：产品（自研项目）

## 一、一句话定义

Sender 是一套「安卓通知采集器 + 自托管查询服务端」：手机端通过系统通知使用权（NotificationListenerService）把每条通知的发送者、内容、时间采进本地 Room 库，按 App 开关过滤后批量上报给自托管的 Go 服务端；服务端提供账号、设备绑定与 RFC 8628 设备码 OAuth 授权——AI agent 先拿到用户输码批准的 access token，再带 token 查询这个人的消息数据。

一句话：**把手机消息流变成用户自有、agent 可授权查询的数据 API。**

这句话里每个词都是挑过的。「自托管」把它跟所有云服务划开，「用户自有」是它的隐私立场，「可授权查询」是它的存在理由——它不满足于把通知转发到另一个屏幕，而是让程序在得到主人允许后，来读你的消息。这个定位在整个品类里是新的，下文会论证。

## 二、纵向分析：从诞生到当下

### 2.1 先说结论：这是一份「一天的历史」

写纵轴之前必须坦白一件事：Sender 这个项目，诞生于 2026 年 8 月 3 日，也就是今天。它的全部代码历史不到 24 小时——服务端 43 个测试全绿，安卓端 29 个测试全绿，生产库 `data/messages.db` 里已经躺着真实消息，旧二进制正跑在 `:8080` 上。一个产品的一天，撑不起一万字的编年史。

所以这一章的写法是：**品类的三十年，加上产品的一天**。Sender 不是从真空中长出来的，它站在一条清晰的历史延长线上——短信转发工具、推送服务、自托管运动、AI 助手的系统级吸收，每一段历史都在它的架构里留下了痕迹。讲清楚这条线，这一天才有坐标。

### 2.2 短信转发时代（2008–2018）：需求先于一切

「把手机消息弄到别处去」这个需求，比智能手机本身还老。安卓的转发工具史可以追溯到 Tasker——2010 年 6 月公开，作者 Pent 为 Android Developer Challenge 2 而作，拿了生产力工具类第三名，后来被 João Dias 收购。Tasker 的核心模型是 Profile（应用、时间、位置、事件上下文）触发 Task（动作序列），而它早期最经典的玩法之一，就是「收到短信自动转发到邮箱或另一台设备」[21]。同一时期，João Dias 又做了 AutoRemote 和 AutoNotification（约 2013 年就有成体系的教程），把「采集通知→转发到别处」做成了 Tasker 生态的标准件 [2]。

2011 年 9 月 IFTTT 上线，把这条需求普及给了普通用户——「如果收到短信，那么发一封邮件」是最早的高频 recipe 之一；它的安卓版 2014 年 7 月才姗姗来迟，2020 年转向 Pro 订阅制后引发批评 [3]。

这一代工具解决什么问题？中文社区给出的答案最直白：**备用机**。收验证码的旧手机、不方便随身携带的工作号、双卡双待时代的主备分离——「安卓备用机短信转发」的教程文至今仍在小众软件、个人博客上不断更新 [4][5]。需求是朴素的，工具是笨重的：要装 Tasker、写规则、配邮箱，折腾半天。但就是这个「笨重」，定义了后来所有品类的两条路：一条走向更简单的云服务，一条走向更灵活的开源工具。

### 2.3 制度性土壤：国产安卓的推送困境

如果只有「备用机转发」这种小需求，这个品类不会长成今天的样子。真正给它浇水的，是国产安卓的推送困境。

2012 年 6 月，谷歌发布 GCM（Google Cloud Messaging），取代弃用的 C2DM；2016 年 5 月谷歌 I/O 宣布 FCM 接棒 [7][8]。这套体系在国外运行良好，在中国却长期失灵：谷歌服务退出中国大陆后，无 GMS 的设备收不到 FCM，国内手机普遍阉割 GMS，厂商各自维护系统级推送通道——华为、小米、OPPO、vivo 各建一套，后台保活策略互不兼容，第三方聚合推送服务（如极光）被迫做多通道适配，工程师专门写文章讲这套碎片化 [9]。V2EX 上至今有用户抱怨国产 App 不用 FCM、非要用自家通道 [10]。

这个背景直接催生了两样东西：一是「推送」在国内成了稀缺能力——服务器想给手机发个通知都困难，于是 Bark、PushDeer 这类自托管推送工具在中国有了独特土壤；二是「采集」类工具必须自己解决保活问题——SmsForwarder 内置 frpc 内网穿透、Cactus 保活组件，就是跟厂商的杀后台机制缠斗的产物 [1]。记住这两个伏笔，后文会反复用到。

### 2.4 云服务时代：Pushbullet 的兴衰（2013–2015 高峰）

2013 年，Ryan Oldenburg 上线了 Pushbullet——灵感来自 Chrome to Phone，最初只是跨设备发链接和文件；2014 年 2 月加入 Android 通知镜像，热度在 2014 年 12 月达到顶峰，同年接入 IFTTT、推出 Channels [12]。它一度是安卓用户的「不可或缺」：手机上的通知实时出现在桌面，还能在电脑上直接回复短信。

然后是一切崩坏的起点：2015 年 11 月 17 日，Pushbullet 宣布 Pro 订阅，$4.99/月或 $39.99/年，把原本免费的短信回复、通知镜像塞进付费墙。Reddit AMA 被愤怒的用户淹没，科技媒体一片批评——9to5Google 直言「Pro 太贵」，Computerworld 的标题是「Pushbullet Pro 不该是这样的」[13][15]。热度此后骤降，2020 年起持续收到 Google Play 政策违规通知，功能迭代放慢 [12]。

这个故事的真正意义不是「收费失败」，而是**系统级吸收**：Pushbullet 当年靠第三方 App 提供的通知镜像，后来被微软 Phone Link、谷歌 Quick Share 这些系统能力直接内化。2026 年仍有 Windows 用户发文讨论它的「衰落」，结论是迁移到 Phone Link [11]。第三方做平台级功能，要么被平台收编，要么被平台替代——这条周期律，是理解整个品类命运的钥匙。

### 2.5 开源自托管浪潮（2018–2023）：把控制权拿回来

Pushbullet 的衰落与开源自托管的兴起是同一枚硬币的两面。2018 年起，一批「自己管服务器」的推送/通知工具接连出现，动机几乎一样：云服务靠不住、闭源不可信、官方要么收费要么停摆。

- **Gotify**：2018 年 1 月建仓（Go，MIT），作者直言动机是「开源可选太少且多数已弃坑、要求可自托管」[14]。REST API 发消息 + WebSocket 实时收，Web 仪表盘按「应用」组织，消息持久化到 SQLite/MySQL/PostgreSQL。现 15,547 星。
- **Bark**：中国开发者 Finb 2018 年 3 月建仓（Swift，MIT）——基于 Apple APNs 的「给自己 iPhone 发自定义推送」，`https://api.day.app/:key/:title/:body` 一个 URL 就推出去 [17]。2021 年作者在 V2EX 分享时，需求场景已经明晃晃写着「服务器告警/脚本推送」[18]。现 8,843 星。
- **ntfy**：作者 Philipp C. Heckel，约 2019 年作为个人副业启动，主仓库 2021 年 10 月公开，2021 年 11 月在 r/selfhosted 宣布「可完全自托管」[19][20]。HTTP pub-sub，`curl -d "备份完成" ntfy.sh/mytopic` 就完事，免注册免账号；单 Go 二进制，Docker 镜像约 15MB、空闲内存约 15MB，树莓派随便跑 [20]。2022 年 12 月被 F-Droid 官方博客列为 UnifiedPush 推荐分发器 [21]。现 32,798 星——是这个品类当之无愧的事实标准。
- **PushDeer**：Server酱作者 easychen 牵头、众筹开源，2021 年 12 月建仓，利用 iOS 14+ App Clip 实现「无 App 推送」。动机写得非常直白：**「不依赖微信消息接口（不像 Server酱 那样受腾讯政策影响）」** [23]。现 5,016 星。
- **UnifiedPush**：2022 年 12 月由 F-Droid 官方博客正式发布 [26]。动机是 FCM 专有、依赖 GMS，F-Droid 应用无法内置 FCM 库，开源应用被迫自建长连接（耗电）。它把「推送」解耦为「分发器 + 规范」，ntfy、NextPush 等皆可实现。推动者包括 Nextcloud（2021 年 issue #8684）与 Element/Matrix（2021 年 issue #2743）[27][28]。

这一波浪潮的共同特征：**单二进制、低资源、标准 HTTP、可私有化**。它们不跟平台对抗，而是绕开平台——你的通知、你的服务器、你的数据。

### 2.6 中国顶流：SmsForwarder（2021–）

在中国，这条线长出了自己的顶流：**SmsForwarder（短信转发器）**，作者 pppscn，2021 年 2 月建仓，原型是 TranspondSms [30]。它监控短信、来电、App 通知，按规则转发到 12+ 渠道——钉钉群机器人、企业微信、飞书、邮箱、Bark、Webhook、Telegram、Server酱、PushPlus、手机短信……V3.0 起甚至支持远程主动控制（远程查短信、查通话、查电量）。现 27,350 星 / 3,352 forks，中文圈「备用机神器」的绝对顶流，iplaysoft、CSDN、腾讯云开发者都有教程文 [1][31][32]。

注意 SmsForwarder 的一个关键选择：采集走的是系统通知使用权（NotificationListenerService，Android 4.3+ 标准机制），不是无障碍服务——这条技术路线跟 Sender 一模一样。但它的终点是「转发」：消息到手即外发，不留库、不提供查询、没有账号概念。**「转发」与「入库」的分野，就是 SmsForwarder 与 Sender 的分野**，后文横向部分细说。

### 2.7 AI 原生时代（2024–2026）：系统级吸收的第二轮

2024 年开始，这个品类遭遇了第二次系统级吸收，而这次吸收者带着 AI。

- **Microsoft Recall**：2024 年 5 月随 Copilot+ PC 发布，默认每几秒截屏记录屏幕，引发「隐私噩梦」争议，英国 ICO 介入；2024 年 6 月被迫延期重做，2024 年秋以 opt-in + Windows Hello + 全库加密的形式重启 [33][34]。2025 年 5 月，Signal 的 Windows 客户端默认开启「屏幕安全」反截屏，官方明说就是防 Recall [33]。
- **Apple**：iOS 18.1（2024 年 10 月，Apple Intelligence 首发）上线通知摘要；因为 AI 新闻摘要失真，iOS 18.3（2025 年 1 月）移除新闻类摘要；iOS 26（2025 年 9 月）重新引入，斜体标注「Summarized by Apple Intelligence」[35][36]。
- **Google**：Pixel 9/10 系列 2025 年 11 月随 Feature Drop 推送 AI 通知摘要，由 Android System Intelligence 实现、端侧 Gemini Nano 运行，官方宣传「消息从不发给 Google」，默认关闭、首批仅英文 [37][38]。
- **中国厂商**：小米「超级小爱」2024 年 12 月在澎湃 OS 2 向正式版用户开放，支持识屏和信息摘要 [41]；华为「小艺简报」在 HarmonyOS 6.0+ 明确包含「通知摘要：梳理 App 通知，提炼核心内容」，官方宣称「不泄露、不存储、不滥用」，但暂不支持微信/QQ 摘要 [42]。

这一轮吸收比 Pushbullet 那轮更彻底：系统助手不只是镜像通知，而是**理解通知**。端侧小模型（Gemini Nano）与 NPU 让「通知级 AI」首次可行，把采集从「外挂工具」变成「OS 原生能力」。三方工具的生存空间被系统挤压——但注意一个关键差异：**系统助手读通知，只输出摘要，不输出数据 API**。厂商的黑箱读得懂你的消息，但不会把消息交给你自己的 agent。这个「只读进黑箱、不对外开放」的缝隙，正是 Sender 的立身之地。

### 2.8 Sender 的诞生：VibeCoding 的一天

2026 年 8 月 3 日，Sender 在 `/Users/flashingchen/Coding/VibeCoding/Sender` 诞生。它的生产方式本身，就是这个时代的产物：**VibeCoding——人写任务书，AI agent 并行施工**。

项目的组织方式值得展开。`tasks/` 下躺着两份任务书（`phase2-server.md`、`phase2-android.md`），每份都是给执行 agent 的完整委托：开头写「这活为什么干」「干完世界什么样」「让步顺序」；中间是「我替领导拍的板」——把关键决策提前拍死（OAuth 用 RFC 8628 设备码、user_code 8 位去 0/O/1/I、access_token 7 天、库中只存 sha256 哈希、密码 bcrypt、唯一新依赖 `golang.org/x/crypto`）；然后是「界限」——白名单文件列表，白名单外只读不碰；然后是防作弊点名（不许删测试、不许 skip、不许 `|| true`）；最后是止损规则（同一验收连败 3 次换下一项，跑满轮数即停如实汇报）。

两个 agent 并行施工：服务端 agent 管 `server/`（Go 模块），安卓端 agent 管 `android/`（Kotlin 工程），跨端的消息契约在任务书里拍死——`client_msg_id = 包名:通知key:postTime毫秒`、单批 ≤500、2xx 才算成功、成功才标 `synced=1`。服务器 agent 在 `:8080` 上跑着旧二进制、生产库里有真实数据，任务书专门划了红线：生产库只许增量迁移，测试一律临时库。

一天之内，两阶段落地：

**阶段一（采集管道）**：安卓端 NotificationListenerService 采通知 → Room 去重入库 → 按 App 列表查看/开关 → WorkManager 上报（新消息触发 + 30 分钟兜底，指数退避重试，服务端幂等去重）。单测 29 个全绿——去重和开关过滤是在 Robolectric + 真 SQLite 上验证的，因为 Room DAO 在纯 JVM 上无法实例化，这是 BLOCKED.md 里明确记录过、拍板引入的测试依赖。服务端 25 个测试全绿。

**阶段二（账号与授权）**：users/sessions/device_grants/tokens 四张表；RFC 8628 设备码授权全流程——agent 发起 `device_authorization` 拿 8 位 user_code，用户在网页登录输码批准，agent 每 5 秒轮询 `oauth/token`，拿到 7 天有效的 access_token 后带 token 查询 `/api/v1/messages` 与 `/api/v1/apps`。设备上报加账号绑定门槛：未绑定 403，绑定 = 网页上凭 device_id + secret 提交（证明手机在你手里）。内嵌中文 Web UI（html/template，零外部资源，embed 进二进制）。测试从 25 增到 43，全绿。README 新增「给 agent 的接入说明」——四步：拿授权 → 用户输码 → 轮询 token → 带 token 查。

这就是全部历史。两个阶段加起来，不到一天。但它浓缩了这个品类 15 年的所有教训：自托管（ntfy/Gotify 的部署哲学）、标准协议（RFC 8628 而不是自造轮子）、数据主权（Pushbullet 被吸收的教训 + Recall 的隐私争议）、以及一个全新的消费端（AI agent）。

### 2.9 决策逻辑：哪些选择锁定了未来

读任务书和代码，能还原出一组明确的决策逻辑：

**为什么无 CGO？** `modernc.org/sqlite` 是纯 Go 实现，构建不需要 gcc。收益是单二进制 + 跨平台交叉编译 + Docker 多阶段构建干净利落。这是自托管浪潮的标准哲学——ntfy 的镜像 15MB、树莓派可跑，Sender 的 Dockerfile 也是同一路数。这个决策锁定了「部署零摩擦」，代价是 SQLite 性能上限（单机单写），对个人数据量级完全够用。

**为什么 RFC 8628 而不是自造授权协议？** 设备码授权是 OAuth 2.0 官方扩展，专为「无输入能力的设备」设计——智能电视、命令行工具、以及 AI agent。任意非空 client_id 即可发起（公共客户端，agent 零预注册），风险由「人类输码确认」兜底。这个选择让任何 OAuth 客户端都能接入，不需要为 Sender 写 SDK——标准就是最大的生态。

**为什么读写双轨？** 设备 secret 只管写（注册、上报），OAuth token 只管读（查询）。阶段一「裸读」→ 阶段二「必须授权」，是有意的 breaking change。设备持有者与数据所有者被明确区分：手机证明「你拥有这个设备」，token 证明「你被账号主人授权」。这是整个设计里最接近「数据主权」的一笔。

**为什么手写 JSON、零外部资源？** 安卓端 payload 用手写序列化（固定形状、可控无多余字段），不引 JSON 库；Web UI 用 html/template、CSS 内联、模板 embed 进二进制。动机都是同一个：**依赖越少，越经得起时间**。

**哪些选择埋下了隐患？** 最大的两个：一是**无前台服务**——通知监听靠系统绑定 NLS，被「强行停止」后监听失效，需要重开 App，README 的已知限制里写得很诚实；二是**无真机冒烟**——`adb devices` 为空，全部验证靠 JVM 测试 + Robolectric，采集链路在真实机型的厂商 ROM 上表现未知。这两个都是「一天工期」的时间成本，它们会成为横向对比里的关键短板。

### 2.10 阶段划分

| 阶段 | 时间 | 核心特征 | 核心矛盾 |
|---|---|---|---|
| 萌芽 | 2026-08-03 前 | 想法成形：给 agent 提供可授权查询的手机消息数据 | 品类无人做过，需求真伪未验证 |
| 建设期 | 2026-08-03 | VibeCoding 极速孵化，phase 1 采集管道 + phase 2 账号授权一天落地 | 速度 vs 可靠性（无前台服务、无真机） |
| 现状 | 2026-08-03 晚 | 43+29 测试全绿，生产实例在跑，agent 接入路径打通 | 0 用户、0 社区、0 验证 |

诚实地说，Sender 还在「建设期」的尾巴上，连「现状」都是几个小时前的。纵轴的最后一格，是空着的——这恰恰是这份报告最有意思的地方：我们不是在复盘一个成熟的赢家或输家，而是在给一个出生 24 小时的产品画它的坐标系。

## 三、横向分析：竞争图谱

### 3.1 竞品全景：场景C，竞品充分，但都不在同一个象限

以 2026 年 8 月为切面，Sender 的竞争环境属于场景C（竞品充分），但要先画一张坐标图，因为**几乎所有竞品都只占了一个象限**。

把「消息」这个对象放在横轴上：**推（push）** vs **拉（pull）**；把「数据去向」放在纵轴上：**转发/镜像** vs **入库/查询**。四个象限：

| 象限 | 代表 | 特征 |
|---|---|---|
| 推→手机（转发） | ntfy、Gotify、Bark、PushDeer、Pushbullet | 服务器/脚本给手机发通知，`curl` 一行即发 |
| 手机→别处（转发） | SmsForwarder、Tasker/AutoNotification、Notification Forwarder | 手机采集通知，即时转发到聊天工具/Webhook |
| 手机→手机/电脑（镜像） | Pushbullet、KDE Connect | 通知镜像到另一块屏幕，实时但不留档 |
| 手机→入库→授权查询 | **Sender** | 采集入库，账号+OAuth 授权，agent 带 token 查询 |

竞品名单很长：SmsForwarder（27,350★）、ntfy（32,798★）、Gotify（15,547★）、Bark（8,843★）、PushDeer（5,016★）、Pushbullet、KDE Connect（3,888★ 镜像仓库）、Tasker/MacroDroid/AutoNotification、Notification Forwarder 等小众应用 [1][14][17][19][22][25]。但深挖之后会发现，Sender 真正要对比的不是某一个工具，而是四个象限各自的生活方式——每个象限的玩家都「活成了什么样」，决定了 Sender 的空白到底是不是空白。

### 3.2 SmsForwarder：最接近的邻居，方向相反

27,350 星、3,352 forks、每周构建版持续更新，中文圈「备用机神器」的顶流 [1]。技术路线上它跟 Sender 同源：NLS 采集（不是无障碍），Kotlin 实现。但它的灵魂是「转发」：规则引擎匹配（App、关键词、正则）→ 12+ 渠道外发（钉钉/企业微信/飞书/Telegram/邮箱/Webhook/Server酱/PushPlus…）。V3.0 加了远程主动控制，v3.3 加了自动任务——越做越像一个「手机自动化中继」。

用户口碑：iplaysoft、CSDN 教程文铺天盖地，评价集中在「免费开源」「规则灵活」「备用机必备」[31][32]。GitHub Issues 里最常见的抱怨是：短信/来电号码过长无法转发、正则匹配/替换转验证码出错、部分机型转发失败（官方为此维护「常见问题」Wiki）[29]。官方声明 APK「仅用于测试与学习、禁止商用」并附反诈提醒——它把自己定位成个人玩家工具，不是商业产品。

跟 Sender 的对比，一句话就能说清：**SmsForwarder 是「即时转发」，Sender 是「入库 + 授权查询」**。SmsForwarder 的消息到了钉钉群就没了，没有账本、没有历史、没有 API；它永远无法回答「上周三下午微信里张先生说了什么」这种问题——不是能力问题，是设计问题，它从来没想过要回答。但反过来，SmsForwarder 有 Sender 没有的东西：12+ 渠道、规则引擎、远程控制、保活全家桶（frpc/Cactus）、以及 27k 星背后的海量机型适配经验。备用机场景的痛点（验证码转发、来电通知），SmsForwarder 已经吃得干干净净。

### 3.3 ntfy 与 Gotify：自托管推送的两极

**ntfy**（32,798★）是自托管推送的事实标准 [14]。HTTP pub-sub，`curl -d "备份完成" ntfy.sh/mytopic` 即可；免注册、免账号、topic 即频道；单 Go 二进制，Docker 镜像约 15MB、空闲内存约 15MB；官方 App 覆盖 Android/iOS（Play、F-Droid、App Store），还有 Web 订阅端（PWA，数据存浏览器本地）[14][15]。它是 UnifiedPush 的参考实现，客户端侧端到端加密、5 级优先级、附件、定时发送。商业化做得克制：自托管永远免费，ntfy.sh 托管服务按量收费（Supporter $6/月 2,500 条/天起）[16]。作者在 FAQ 里自认 iOS App 简陋、官方服务器单机托管无冗余、best-effort 可用性 [15]。

**Gotify**（15,547★）走另一条路 [17]。REST API 发消息 + WebSocket 实时收，Web 仪表盘按「应用」组织消息，消息持久化到 SQLite/MySQL/PostgreSQL——**它真的会存**，还支持插件系统、CLI。没有官方 iOS App，维护者以 Apple 开发者年费 $99 为由拒绝开发 [18]。社区共识的对比是：ntfy 赢在免账号 topic、UnifiedPush、云端回退、iOS、E2E 加密；Gotify 赢在仪表盘、插件、消息持久化与归档 [19][20]。

这两个跟 Sender 的关系很微妙：**方向完全相反**（它们是「推→手机」，Sender 是「手机→库」），但**部署哲学同源**（单二进制、自托管、低资源）。一个 Sender 用户大概率家里也跑着 ntfy。它们证明了「自托管消息基础设施」是一个健康、有付费意愿的市场——ntfy 的 Pro 订阅就是证据。但它们的消息来自脚本和服务，不是来自你的手机；它们没有采集器，更谈不上授权查询。

### 3.4 Pushbullet 与 KDE Connect：镜像时代的活化石

Pushbullet 还在，但已经是活化石。免费版每月 100 条消息、存储 2GB；Pro $4.99/月或 $39.99/年 [6]。r/Pushbullet 常年是通知镜像失灵的求助帖（2015/2019/2023 多轮）[7]，2026 年的用户讨论已经变成「迁移到 Phone Link」[8]。它的功能（通知镜像、剪贴板、文件传输）被系统能力逐项吸收，Pro 付费墙在 2015 年就透支了用户信任 [9][13]。

KDE Connect 是另一种活法：同网段设备互联，通知同步只是它的功能之一（还有共享剪贴板、文件传输、多媒体遥控、虚拟触控板、远程命令、短信/MMS 读写），Play 商店 100 万+ 下载 [22][23]。免费、开源、强大，但受限于同一局域网，Windows/macOS 非官方支持，而且它从诞生起就是「设备互联工具」，不是「数据管道」。

这两个代表着一个 Sender 已经明确放弃的方向：**镜像**。把通知搬到另一块屏幕上，是上个时代的答案；把通知变成可查询的数据，是下个时代的答案。Pushbullet 的衰落史就是这条分界线的形成史。

### 3.5 Bark 与 PushDeer：国产 iOS 推送，两代人的命运

Bark（8,843★）：中国开发者 Finb 的作品，Swift，MIT [21]。`https://api.day.app/:key/:title/:body` 一个 URL 推给自己的 iPhone，完全依赖 Apple APNs（作者宣称不耗电），支持分组、自定义图标/声音、时效性通知、critical alert。中文圈生态丰富——浏览器插件、CLI、GitHub Action 一应俱全。短板也明确：只覆盖 Apple 设备，自托管仍需自备 APNs 证书 [21]。

PushDeer（5,016★）是更有戏剧性的样本：Server酱作者 easychen 牵头，用 iOS App Clip 实现「无 App 推送」，一度是国产自托管推送的希望。但 2026 年的 README 上赫然写着：**官方 Android 版因接口权限停止无法使用（issue #150），部分开发者退出、项目已不再维护，官方建议改用 Server酱³** [24][25]。一个开源项目从众筹启动到公开宣布死亡，五年不到。

这两个样本对 Sender 的启示很冷：**iOS 是另一个生态**（Bark 证明 APNs 通道足够养活一个工具，也证明 Android 侧的能力无法平移过去）；**无商业模式的个人开源项目会死**（PushDeer 的教训是「官方 API 继续服务但不更新」——维护者靠爱发电的极限就在那里）。

### 3.6 Tasker / MacroDroid：自动化管线的天花板

Tasker（2010 年，ADC2 三等奖，现属 João Dias）是「采集→处理→转发」管线的天花板：Notification 事件 + AutoNotification 插件可以拦截通知、提取 `%antext`、转发到任何渠道；2025 年 5 月起还加了 AI 生成，2026 年 2 月加入 BeanShell/Shizuku [10]。一次性买断 $4/许可 [11]。MacroDroid 是它的新手版：免费 5 条宏 + 广告，Pro 买断去广告无限宏，Play 商店 3000 万+ 下载 [12]。

这类工具的真实生态位：**技术玩家的瑞士军刀**。能干一切，但一切都是手动拼装——没有统一的数据格式、没有服务端、没有授权概念。Tasker 用户想要「agent 查询我的消息」，得自己写 HTTP 动作、自己搭服务器、自己处理认证。能力上限最高，交付下限也最低。Sender 的差异化在这里反而最清晰：**把 Tasker 用户要折腾一周的活，做成开箱即用的标准管道**。

### 3.7 相邻竞争者：系统级 AI 助手与记忆层

真正的威胁不在工具象限里，在系统层。2025–2026 年，手机厂商的 AI 助手已经能读通知：

- Pixel 9/10 的 AI 通知摘要：端侧 Gemini Nano，官方承诺「消息从不发给 Google」，但默认关闭、首批仅英文 [37][38]；
- 华为小艺简报（HarmonyOS 6.0+）：明确包含「通知摘要」，数据源含消息通知、短信、备忘录、日历，官方宣称「不泄露、不存储、不滥用」，但暂不支持微信/QQ 摘要 [42]；
- 小米超级小爱（2024 年 12 月开放）：识屏 + 信息摘要，2026 年 7 月已能一句话操控微信发消息 [41]。

这些助手证明了两件事：一是通知数据确实值钱，二是**厂商只想把数据喂给自己的模型，不会开放给第三方 agent**。系统助手的输出是摘要，不是 API；是黑箱，不是数据主权。它们不会取代 Sender，但会教育用户「AI 读通知」这个概念——这反而替 Sender 做了市场教育。

另一侧是 agent 记忆层：Mem0（2023 年创立，2025 年 10 月融资 $24M，开源 SDK + 托管 API）、Letta（MemGPT 论文的产物，2024 年 9 月 $10M Seed）、Personal.ai。它们全部依赖「用户显式喂数据」——对话历史、任务记录，没有一家做设备端自动采集 [43][44]。它们与 Sender 是互补关系：记忆层缺的正是 Sender 提供的那条自动管道。MCP（Model Context Protocol，2024 年 11 月 Anthropic 发布）生态里，个人数据类 server 已经成型：Google 官方 Workspace MCP（Gmail/Calendar/Drive）、社区 WhatsApp MCP server（Go 单二进制封装 whatsmeow，暴露 41 个工具）[45][46][47]。**但没有面向系统通知通道的 MCP 标准**——这正是 Sender 理论上最顺手的接口层。

### 3.8 生态位：空白是真实的，但要看清空白为什么存在

回到那张象限图。竞品群没有一个同时具备三个要素：**手机端采集（NLS）+ 自托管存储 + 面向 agent 的授权查询（OAuth 设备码）**。Gotify 有存储和查询 API，但它是推送接收端，没有采集器，也没有第三方授权概念；SmsForwarder 有采集，但即时转发不留库；ntfy 有 API，但方向相反。Sender 占着第四象限的独苗位置。

这个空白为什么存在？我的判断是三个原因叠加：**需求太新**（「agent 读我的手机消息」是 2024 年 MCP 之后才成形需求，2026 年仍在早期）；**自托管门槛**（要同时会写安卓和 Go，还要懂 OAuth，这个技能组合极少）；**商业回报不明**（个人数据管道没有清晰的付费模式，风投不会投，个人开发者又没动力）。空白不等于蓝海——它也可能是「没人找到钱」的荒原。但对一个自用优先的 VibeCoding 项目来说，这个空白刚刚好。

### 3.9 用户口碑：竞品的声音，与 Sender 的沉默

竞品的真实口碑，每个都有一句代表性的槽点：Pushbullet 是「通知镜像又失灵了」（Reddit 十年如一日）[7]；SmsForwarder 是「号码太长转发不了」「正则改验证码出错」（GitHub Issues）[29]；ntfy 是「iOS App 简陋」（作者自己承认）[15]；Gotify 是「没有 iOS」[18]；PushDeer 是「停维护了」[24]。共通规律：**这些工具的用户抱怨的永远是边缘功能，没人抱怨核心定位**——因为每个工具的定位都足够窄，窄到核心需求被满足得死死的。

Sender 的用户口碑是零。不是差评，是**沉默**——0 用户、0 社区、0 真机验证。这是横向对比里最刺眼的一行：所有竞品都有五年以上的用户积累和机型适配，Sender 有且只有 43+29 个测试。它的「用户」目前只有一个：开发者本人，以及他那台跑着生产库的服务器。这份报告写到这里，必须把这条如实摆出来——优点清单和缺点清单里，都有这一条的位置。

### 3.10 趋势判断：走向哪边

基于横向对比，Sender 的走向取决于它选择跟哪个象限结盟：

- **机会**：agent 经济在扩张（MCP 标准化、记忆层融资不断），「个人数据管道」是记忆层的上游刚需；自托管玩家（ntfy 的 3 万星社区）是天然的早期用户池；系统助手的市场教育让「AI 读通知」不再骇人。
- **风险**：系统级吸收随时可能把第四象限也吃掉——如果哪天 Gemini/小爱开放了通知数据 API（或与 MCP 打通），第三方管道会被釜底抽薪；合规红线（通知内容 = 敏感个人信息，见 4.4）；以及最现实的：**单点维护者的倦怠**，PushDeer 就死在这。
- **最可能的短期路径**：先在自托管极客圈里以「ntfy 的邻居」身份被认识——不是替代任何人，而是补上它们都没有的那一段。

## 四、横纵交汇洞察

### 4.1 历史如何塑造了当下的位置

把纵轴和横轴叠起来，Sender 的位置一下子清晰了：**它站在品类第三次浪潮的起点上**。

第一次浪潮（2010–2015）是规则引擎与云服务：Tasker/IFTTT 教会用户「消息可以流转」，Pushbullet 验证了「镜像」的巅峰与死亡方式——被系统吸收。第二次浪潮（2018–2023）是自托管反叛：Gotify/Bark/ntfy/PushDeer 用单二进制和标准 HTTP 把控制权夺回用户手里，UnifiedPush 把「不绑死单一公司」变成协议。第三次浪潮（2024–）是 AI 原生：系统助手读通知做摘要，记忆层融资不断，MCP 把「数据管道」变成标准件。Sender 的诞生时间点（2026 年 8 月）恰好在第三次浪潮的早期——它捡起了前两次浪潮的所有遗产（NLS 采集、单二进制自托管、标准 OAuth），指向第三次浪潮的缺口（agent 可授权查询的个人消息数据）。

这个位置不是运气，是历史给的：如果 Sender 早生三年，它面对的是「agent 在哪」的尴尬；晚生三年，可能就要面对系统级 API 的碾压。2026 年这个窗口——MCP 已成型、记忆层缺数据、厂商 API 未开放——是它唯一不早不晚的时刻。**窗口期是我能给出的最重要的判断：这个位置的有效期，可能只有 12–24 个月。**

### 4.2 优势清单：每一条都有历史根源

| 优势 | 事实依据 | 历史根源 |
|---|---|---|
| 部署零摩擦 | 无 CGO 单二进制 + Docker 多阶段，SQLite 纯 Go | ntfy/Gotify 证明过的自托管哲学 |
| 授权走标准 | RFC 8628 设备码，agent 零预注册，人类输码兜底 | 品类史上一堆自造协议的死法；OAuth 生态即兼容性 |
| 读写分离的数据主权 | 设备 secret 只管写，OAuth token 只管读，token 哈希存储 | Pushbullet 被吸收的教训 + Recall 隐私争议的直接回应 |
| 幂等可靠的上报 | client_msg_id 全局去重、指数退避、2xx 才标 synced | 无真机条件下的防御性设计（把不确定性挡在协议层） |
| 离线可部署 | 内嵌 Web UI 零外部资源，ALLOW_REGISTRATION 开关 | 自托管浪潮「不依赖任何平台」的价值观 |
| 契约先行的工程质量 | 任务书拍死接口，双 agent 并行，43+29 测试全绿 | VibeCoding 工作法本身就是优势——它让一天工期不翻车 |

注意最后一条：**VibeCoding 不仅是生产方式，也是竞争壁垒**。这个品类（自托管 + 双端 + OAuth）的传统开发成本是数周；Sender 用一天交付。如果这个工作流可复制，它的迭代速度会让所有个人开发者竞品望尘莫及——这不是功能优势，是速率优势。

### 4.3 劣势清单：同样有历史根源

| 劣势 | 事实依据 | 历史根源 |
|---|---|---|
| 采集可靠性存疑 | 无前台服务，强停后 NLS 失效需重开；无真机冒烟 | 「一天工期」的时间取舍——省掉了最贵也最不可控的验证环节 |
| 微信生态摩擦 | 「通知显示消息详情」需手动开启且无法程序化检测 | 微信的封闭性是外部约束，但 Sender 没做引导兜底 |
| 平台单一 | 无 iOS（Bark 证明那是另一个生态），无桌面端 | 单平台起步的务实选择，也是天花板 |
| 0 用户 0 验证 | 无社区、无机型适配、无真实使用反馈 | 出生 24 小时——这条劣势会随第一个外部用户到来而部分消解 |
| 无 MCP 适配 | 只有裸 HTTP API，agent 接入需自己写胶水 | 诞生于 MCP 之后却尚未接 MCP——时间差，24 小时内可补 |
| 合规风险未处理 | 通知内容 = 个人信息，采集需显著告知 + 单独同意 | 见 4.4，这是整个品类（包括所有竞品）都没解决的题 |

最值得警惕的是第一条和最后一条的组合：**采集可靠性是产品底线，合规是生存底线**。SmsForwarder 用 frpc/Cactus 对抗厂商杀后台，那是 27k 星换来的血泪经验；Sender 目前只有 README 里一句「被强行停止后需重开 App」。

### 4.4 合规：悬在所有竞品头上的同一把刀

横向调研里最冷的发现：2025 年中国被通报违规的 App 达 3852 款，比 2024 年的 1529 款增长 152%，典型违规点就是「首次运行未显著提示隐私政策、默认同意」[48]；《个人信息保护法》2021 年施行，2023–2024 年 App 备案制落地，未备案不得从事 App 互联网信息服务 [49]。欧盟侧，通知内容属个人信息（GDPR Art. 4(1)），爱尔兰 DPC 因透明度违规罚 WhatsApp €5.5M，荷兰 AP 罚 Netflix €4.75M [50]。

这条刀不是只悬在 Sender 头上——SmsForwarder 采集短信内容、Pushbullet 镜像所有通知，谁都没能优雅解决。但 Sender 的处境更微妙：**它的卖点恰恰是把这些数据交给第三方程序（agent）**，这在「单独同意」的监管语境下是最高风险动作。自托管 + 用户显式授权（输码确认）的设计与监管方向吻合，但采集端 App 的隐私声明、同意流程目前是一片空白——README 里没有、代码里没有、任务书里也没有。这是我判断必须最先补的短板，优先级高于任何新功能。

### 4.5 竞品的纵向对比：为什么它们长成了现在这样

把主要竞品放回时间线，各自的宿命一目了然：Pushbullet 生于云服务黄金时代，死于系统吸收——它没有自托管基因，平台一发力就无处可退；SmsForwarder 生于国产推送困境，长于备用机刚需，它的「即时转发」设计源于「验证码不能等」的物理需求——转发是实时的，入库是后置的，所以它天然不长查询；ntfy 生于开发者自用，长于 r/selfhosted 的极客口碑，它的 HTTP pub-sub 极简设计让「推」的体验做到极致，但也让它从未想过「拉」；PushDeer 生于「不依赖微信」的理想，死于无商业模式——它证明了个人开源项目的生存底线。

Sender 与它们最大的不同不在功能，在**出生方式**：它是唯一一个从设计之初就把「agent」写进定位的产品。竞品都是「人的工具」，Sender 是「程序的管道」——这个差异贯穿了每一次架构决策（OAuth 设备码、token 哈希、读写分离），也决定了它在四象限图里的独苗位置。

### 4.6 未来推演：三个剧本

**剧本一（最可能）：利基站稳，成为自托管 agent 数据管道的标准件。** 逻辑：agent 经济持续扩张，记忆层（Mem0/Letta）需要自动数据源；Sender 补上 MCP 适配器后，成为「手机消息 → MCP server」的最短路径；在 r/selfhosted 和中文极客圈以「ntfy 的邻居」身份积累几百个自托管用户；维持单点维护 + 社区 PR 的轻模式。时间窗 12–24 个月，风险是维护者倦怠（PushDeer 之死）。这剧本里 Sender 不会发财，但会活着，且活得有用。

**剧本二（最危险）：系统级吸收 + 合规收紧的双杀。** 逻辑：Pixel 的端侧通知摘要、华为小艺简报已经证明厂商在吞这个能力；如果任一家厂商（或 Google 通过 MCP）开放通知数据 API，第四象限的「独苗」瞬间变成「多余」——用户为什么不直接用系统能力？同时，若通知数据被监管界定为敏感个人信息、采集与转发需逐项单独同意，整个品类（含 Sender）的合规成本陡增，个人开发者无力承担。这剧本下 Sender 会像 Pushbullet 一样「还活着但不再被需要」。

**剧本三（最乐观）：成为个人数据主权层的枢纽。** 逻辑：厂商 API 始终是黑箱（不开放给第三方 agent），而 agent 需要的是「用户自有」的数据——这正是 Sender 的定位；记忆层产品（Mem0 类）主动集成它作为数据源；MCP 生态里出现「官方推荐」的 notification 数据 server；托管版（Sender Cloud）+ Pro 订阅复制 ntfy 的商业模式。这剧本需要两个前提：系统级 API 长期不开放第三方接入（概率中等），以及 Sender 在窗口期内建立用户与口碑护城河（概率偏低，取决于 4.2 的速率优势能否持续）。

三个剧本共用的变量只有一个：**时间**。窗口期是真实的，12–24 个月里必须完成三件事——补可靠性（前台服务/真机验证）、补合规（隐私声明与同意流程）、补接口（MCP 适配器）。做完这三件，剧本一的概率显著上升；做不完，剧本二自动生效。

### 4.7 回环：从 Pushbullet 到 Sender，品类的第三次轮回

写到这里，可以回到开头那句话了：**把手机消息流变成用户自有、agent 可授权查询的数据 API**。

这个品类二十年的历史，其实是同一个故事的三个版本：第一次，人想在自己的设备之间搬消息（Tasker → Pushbullet），平台赢了；第二次，人想绕开平台搬消息（Gotify → ntfy → UnifiedPush），自托管赢了，但没人消费这些数据；第三次，程序开始替人读消息（Recall、通知摘要、MCP），平台又回来了——而这一次，平台只输出摘要，不输出数据。

Sender 站在第三次轮回的缺口上。它能不能填上，取决于一件与代码无关的事：**这个品类第三次轮回的赢家，会不会仍然只有一个平台？** 我的判断是：如果自托管社区能在这个窗口期把「通知数据管道」做成像 ntfy 一样的事实标准，答案就是「不会」。二十年前 Tasker 教会人们消息可以流转，十年前 Pushbullet 教会人们流转会被收编，五年前 ntfy 教会人们可以自己管——现在轮到 Sender 这代人回答：自己管的数据，能不能喂给自己的 agent。

这个问题，今天的 43 个测试回答不了。但它值得被回答——因为这可能是这个品类最后一次还能由个人开发者回答的机会。

## 五、信息来源

（全部来源访问于 2026-08-03）

**本地一手来源（代码与文档，直接核验）**
- Sender 项目目录：`server/README.md`、`server/PROGRESS.md`、`server/BLOCKED.md`、`tasks/phase2-server.md`、`tasks/phase2-android.md`、`android/README.md`、`android/PROGRESS.md`、`android/BLOCKED.md`、`server/go.mod`；`go test ./...` 实测 43 个测试全绿（0 skip），安卓端 29 个全绿（见 `android/PROGRESS.md` 2026-08-03 记录）

**竞品与品类**
1. SmsForwarder 仓库：https://github.com/pppscn/SmsForwarder （27,350★/3,352 forks）
2. SmsForwarder 常见问题 Wiki：https://github.com/pppscn/SmsForwarder/wiki/%E3%80%90%E5%BF%85%E8%AF%BB%E3%80%91%E5%B8%B8%E8%A7%81%E9%97%AE%E9%A2%98
3. SmsForwarder Issues：https://github.com/pppscn/SmsForwarder/issues
4. iplaysoft 介绍：https://www.iplaysoft.com/smsforwarder.html
5. 小众软件 短信转发教程：https://www.appinn.com/how-to-forward-sms-in-android/
6. Pushbullet 定价页：https://getpulsesignal.com/pricing/pushbullet
7. r/Pushbullet 镜像失灵帖：https://www.reddit.com/r/PushBullet/comments/r1cg0z/notification_mirroring_not_working_even_test/
8. Pushbullet 衰落讨论：https://windowsforum.com/threads/pushbullet-decline-to-phone-link-a-practical-windows-migration-guide.393652/
9. TNW 评 Pushbullet Pro：https://thenextweb.com/news/pushbullet-launches-a-5-pro-tier-but-thats-bad-news-for-free-users
10. Tasker 维基：https://en.wikipedia.org/wiki/Tasker_(application)
11. Tasker 官网直购：https://tasker.joaoapps.com/userguide/en/faqs/faq-direct-purchase.html
12. MacroDroid Play 页：https://play.google.com/store/apps/details?id=com.arlosoft.macrodroid
13. howtogeek Pushbullet 回顾（2026-07）：https://www.howtogeek.com/pushbullet-was-an-indispensable-android-app-until-pro-version/
14. ntfy 仓库：https://github.com/binwiederhier/ntfy （32,798★）
15. ntfy FAQ：https://docs.ntfy.sh/faq/
16. ntfy.sh 定价：https://ntfy.sh/
17. Gotify 仓库：https://github.com/gotify/server （15,547★）
18. Gotify 无 iOS 讨论：https://github.com/gotify/server/issues
19. ntfy vs Gotify 对比：https://selfhosting.sh/compare/ntfy-vs-gotify/
20. r/selfhosted ntfy vs Gotify：https://www.reddit.com/r/selfhosted/comments/shw73e/difference_between_ntfy_and_gotify/
21. Bark 仓库：https://github.com/Finb/Bark （8,843★）
22. KDE Connect：https://github.com/KDE/kdeconnect-kde ；https://play.google.com/store/apps/details?id=org.kde.kdeconnect_tp
23. PushDeer 仓库：https://github.com/easychen/pushdeer （5,016★）
24. PushDeer issue #150（安卓版停用）：https://github.com/easychen/pushdeer/issues/150
25. V2EX Bark 分享帖：https://v2ex.com/t/804506

**品类史**
26. GCM/FCM：https://en.wikipedia.org/wiki/Google_Cloud_Messaging ；https://developers.googleblog.com/google-cloud-messaging-and-firebase/
27. 极光工程师谈国产推送碎片化：https://xie.infoq.cn/article/637988006a8b0b0015072c376
28. V2EX FCM 讨论：https://www.v2ex.com/t/753226
29. F-Droid UnifiedPush 发布：https://f-droid.org/en/2022/12/18/unifiedpush.html
30. Nextcloud/Element 推动 UnifiedPush：https://github.com/nextcloud/android/issues/8684 ；https://github.com/element-hq/element-android/issues/2743
31. ntfy Changelog 访谈：https://changelog.com/podcast/562 ；https://www.reddit.com/r/selfhosted/comments/qxlsm9/
32. IFTTT 维基：https://en.wikipedia.org/wiki/IFTTT
33. Windows Recall 争议与延期：https://en.wikipedia.org/wiki/Windows_Recall ；https://www.theverge.com/2024/6/13/24178144/microsoft-windows-ai-recall-feature-delay
34. Signal 反 Recall：https://www.malwarebytes.com/blog/news/2024/06/microsoft-recall-delayed-after-privacy-and-security-concerns
35. iOS 18/26 通知摘要：https://en.wikipedia.org/wiki/IOS_18 ；https://www.apple.com/newsroom/2025/06/apple-elevates-the-iphone-experience-with-ios-26/
36. Pixel AI 通知摘要（2025-11）：https://9to5google.com/2025/11/13/pixel-notification-summaries/ ；https://support.google.com/pixelphone/answer/16691280
37. ZDNet Pixel 通知摘要实测：https://www.zdnet.com/article/android-notification-summaries-pixel-hands-on/
38. 小米超级小爱：https://finance.sina.com.cn/tech/roll/2024-12-27/doc-ineawtrv2622605.shtml
39. 华为小艺简报：https://consumer.huawei.com/cn/support/content/zh-cn16075819/

**agent 数据方向**
40. Mem0：https://github.com/mem0ai/mem0 ；融资 https://techcrunch.com/2025/10/28/mem0-raises-24m-from-yc-peak-xv-and-basis-set-to-build-the-memory-layer-for-ai-apps/
41. Letta：https://github.com/letta-ai/letta ；论文 https://arxiv.org/abs/2310.08560 ；融资 https://techcrunch.com/2024/09/23/letta-one-of-uc-berkeleys-most-anticipated-ai-startups-has-just-come-out-of-stealth/
42. Rewind 现状：https://rewind.ai/what-happened-to-rewind/
43. MCP 发布：https://www.anthropic.com/news/model-context-protocol
44. Google Workspace MCP：https://developers.google.com/workspace/guides/configure-mcp-servers
45. 社区 WhatsApp MCP：https://github.com/sealjay/mcp-whatsapp ；https://github.com/TensorBlock/awesome-mcp-servers
46. Home Assistant AI：https://www.home-assistant.io/blog/2025/09/11/ai-in-home-assistant/ ；n8n：https://n8n.io/ai-agents/

**合规**
47. App 备案：https://news.cctv.com/2023/08/08/ARTIVaArWUBVYGBjJwFAJIvF230808.shtml
48. 2025 年违规 App 通报数据（3852 款 vs 1529 款）：https://news.qq.com/rain/a/20260108A0180J00
49. 网信系统约谈数据：http://www.xinhuanet.com/20250225/70d18c2abd74406a8450e7eb7da03934/c.html
50. WhatsApp/Netflix 处罚：https://www.dataprotection.ie/en/dpc-guidance/decisions/inquiry-whatsapp-ireland-ltd-january-2023 ；https://www.edpb.europa.eu/news/dutch-sa-fines-netflix-for-not-properly-informing-customers_en

**方法论说明**

本报告采用横纵分析法（Horizontal-Vertical Analysis）：纵轴追踪品类与研究对象从诞生到当下的完整生命历程（历时分析，源自索绪尔语言学与纵向研究设计），横轴以 2026 年 8 月为切面与竞品系统对比（共时分析、横截面研究设计），最后以两轴交汇产出综合判断。研究于 2026-08-03 完成，由三个并行调研单元联网采集事实（每个关键事实均附来源 URL），本地事实直接核验自项目代码与测试输出。
