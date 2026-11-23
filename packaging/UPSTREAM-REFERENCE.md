# 上游参考与功能映射（PC Web ADB (PWA) 1.1.0）

## 1. 参考项目与许可证来源

| 项目 | 值 |
| --- | --- |
| 名称 | sky adb（Android 手机端 ADB 管理工具） |
| 仓库 | https://github.com/sky22333/skyadb |
| 分支 / 提交 | `main` @ `bbf01698880f3b09c40dd0672b297fe16fd70ef6` |
| 获取时间 | 2026-11-14 |
| 获取方式 | `https://codeload.github.com/sky22333/skyadb/zip/refs/heads/main` |
| 许可证 | GPL-3.0（`LICENSE` 首行：Copyright (C) 2026 github.com/sky22333） |
| 技术栈 | Kotlin / Jetpack Compose / MIUIX / Kadb 2.1.5 / kadb-mdns / scrcpy-server 5.0.1 / DataStore / OkHttp |
| 源码快照 | 源树的 `reference/skyadb/`（不进入二进制 .deb） |

上游是 **Android APK**：`./gradlew assembleRelease` 只能产出 `.apk`，无法在
Debian 上安装或运行，且其 ADB 协议实现（Kadb）与投屏（scrcpy-server）都
依赖 Android 运行时。因此本任务采取的方案是：

1. 先完整获取上游开源代码并快照到 `reference/skyadb/`，据此提取功能清单、
   页面结构与中文文案；
2. 用 Go 1.20 + 内嵌 Web UI **独立实现**同等工作流的 Linux 桌面客户端；
3. 编译该实现并用 Debian 规范打成 `.deb`。

本包**不包含**上游任何 Kotlin/Java 源码、Kadb、MIUIX、scrcpy-server 或
Android 资源。由于是功能层面的改编，实现部分按 **GPL-3.0-or-later** 发布，
并在 `copyright`/`source.tar.gz` 中提供完整对应源码与署名。

## 2. 核心功能范围：保留 / 调整 / 移除

### 保留（一一对应）

| SkyADB 页面 / 功能 | PC 端对应 |
| --- | --- |
| 无线调试配对、连接、断开、最近设备记录 | 设备连接页：配对 / 连接 / 断开 / 最近设备（持久化到 `~/.config/pcwebadb/config.json`） |
| 局域网自动发现、网段扫描 | 局域网发现：本机 /24 自动探测 + 自定义 CIDR + 并发 TCP 端口探测 + 自动 `adb connect` |
| 设备基础信息与连接状态 | 设备详情页：`getprop` / `wm size` / `dumpsys battery` / `df` / `ip` 等 |
| 应用列表、分类筛选、搜索、启动、停止、卸载 | 应用管理：用户/系统/全部筛选、包名搜索、启动/强停/卸载/冻结/启用 |
| 本地 APK 安装、在线下载 APK 后安装 | 应用管理 → 安装 APK：本地上传 + URL 下载后 `adb install` |
| 目标设备文件管理、目录浏览、上传、下载 | 文件管理：`adb shell ls` 解析、`adb push/pull`、新建/重命名/删除 |
| 截图预览，需要时再保存 | 设备截图：`adb exec-out screencap -p` 先预览，再下载 PNG |
| 系统日志查看与复制 | Logcat：`logcat -d` 快照 + SSE 实时流 + 一键复制 |
| 模拟物理按键、虚拟遥控器 | 虚拟遥控器：方向键 / OK / 返回 / 主页 / 音量 / 电源 / 文本注入 |
| 屏幕镜像 | 屏幕镜像（见下方调整） |
| Shell 命令执行与输出查看 | Shell 页 + 常用命令预设 |
| 诊断日志 | 诊断日志（内存环形缓冲，500 条） |
| 默认端口、连接超时、命令超时、镜像画质、主题 | 设置页全部保留，另增扫描并发、扫描网段、默认文件目录、自定义 adb 路径 |

### 调整（因平台差异）

| 项 | 上游（Android） | 本实现（PC） |
| --- | --- | --- |
| ADB 协议 | 进程内 Kadb 2.1.5 实现 | 调用包内官方 `adb` 37.0.1（支持 `adb pair`），系统自带 1.0.39 不支持配对 |
| 发现方式 | kadb-mdns + `AdbProtocolProbe` | 有界并发 TCP 端口扫描（/16~/30，上限 4096 地址），无需 mDNS 系统服务 |
| USB 连接 | USB OTG 主机模式 + Fastboot | 标准 PC USB 主机，`adb devices -l` 自动枚举；**Fastboot OTG 不移植** |
| 屏幕镜像 | 内嵌 scrcpy-server + MediaCodec 解码 | 调用系统 `scrcpy` 打开独立窗口（`Recommends: scrcpy`），未安装时明确提示 |
| 交互布局 | 底部 4 项导航 + 设备详情页 7 个子页 | 左侧分组导航栏（连接/管理/调试/系统）+ 顶部设备选择器，页面划分保持一致 |
| 视觉 | MIUIX / Material 3 | Material 3 风格 CSS（卡片、圆角、状态徽标、深浅主题） |
| 在线推送文件 | 在线下载文件后 push 到设备 | 保留 URL 安装 APK；通用文件推送改为本地文件选择上传 |
| AI 助手 | OpenAI 兼容 / MCP / Koog | **不移植**（与 ADB 管理目标无关，且需外部密钥） |

### 移除（Android 端特有）

- 本机用户应用导出并安装到目标设备（PC 上没有「本机 Android 应用」）
- USB OTG / Fastboot 协议栈与设备摘要
- 应用真实名称/图标解析（依赖 Android PackageManager + APK 资源解析）
- AI 助手与 MCP 连接

## 3. 安全约束（对齐上游校验器）

- `validateRemoteCommand`：拒绝空命令与控制字符，防止命令夹带；
- `validateRemotePath`：拒绝 `..`、控制字符，禁止对 `/`、`/system`、`/proc`、`/dev` 等根路径执行写操作；
- 所有远端参数经 `shq()` 单引号转义；包名、按键、日志过滤表达式均做白名单校验；
- 窗口与后端之间的内部通信仅监听 `127.0.0.1` 并校验 `Host` 头，阻断 DNS rebinding；
  该端口不对外提供网页服务，用户看到的是 GTK3 原生应用窗口。

## 4. 第三方组件

| 组件 | 版本 | 许可证 | 说明 |
| --- | --- | --- | --- |
| Android SDK Platform Tools `adb` | 37.0.1 (1.0.41) | Apache-2.0 + Android SDK License | 从 `dl.google.com` 官方 zip 原样提取，附 `NOTICE.txt` |
| Go 标准库（编译期） | 1.20.14 | BSD-3-Clause | 纯标准库，无外部 Go 模块；cgo 层仅用于 `dlopen` |
| GTK3 / WebKitGTK（运行期，系统库） | 3.24 / 2.38 | LGPL-2.1+ / BSD-2-Clause 等 | **仅运行时 `dlopen` 动态加载**，不随包分发、不静态链接；缺失时自动降级为独立浏览器应用窗口 |
| scrcpy（运行期，可选） | 系统包 | Apache-2.0 | 仅 `exec` 调用，不随包分发 |
