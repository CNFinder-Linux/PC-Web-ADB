# PC Web ADB (PWA)

跨平台桌面版 ADB 设备管理工具，功能与交互参考 GPL-3.0 的 Android 应用
[sky adb](https://github.com/sky22333/skyadb)（`main` @ `bbf0169`，2026-11-14）。
提供 **Linux（`.deb`，amd64）** 与 **Windows（支持 Windows 7，NSIS 中文安装包）** 两个交付版本。

> 上游是 Android APK（Kotlin / Compose / Kadb / scrcpy-server），不能编译成
> 桌面原生程序。本项目是**独立的 Go 1.20 + 内嵌 Web UI 实现**，
> 上游源码完整快照保存在 `reference/skyadb/` 作为设计参考与许可审计依据。

## 快速开始

### Linux / Debian（amd64）

```bash
./packaging/build-deb.sh                     # 编译 + 打包 + 校验
sudo dpkg -i dist/pcwebadb_1.1.0-1_amd64.deb
# 从应用菜单点击「PC Web ADB (PWA)」，或在终端：
pcwebadb                                    # 打开原生应用窗口；关窗后转入后台托盘
```

Linux 版是**真正的桌面应用**：从应用菜单启动会弹出 GTK3 原生窗口，界面由内嵌的
WebKitGTK 渲染——独立标题栏、独立任务栏图标，没有地址栏、没有浏览器标签页。
GTK3 / WebKitGTK 通过 `dlopen` 运行时加载，缺失时自动降级为 Chromium / Edge /
Firefox 的独立 `--app` 窗口，再降级为纯服务模式。本机回环端口只用于窗口与 Go
后端之间的内部通信，不对外提供网页服务。

无图形会话才需要服务模式：`pcwebadb --headless --port 18888`（或
`--window=none` / `--no-browser`）。也可用 `--window=gtk|browser` 显式指定界面模式；
需要同时跑多个独立服务时加 `--new-instance`。详见
[docs/BUILD.md](docs/BUILD.md) §3.1 与 `man pcwebadb`。

### 后台运行与任务栏小图标

**关闭窗口不再停止服务**。窗口只是隐藏，`adb` 与本地服务继续在后台运行，浏览器里
已打开的管理页面刷新后仍可用；通知区域（任务栏右侧）会出现 PC Web ADB (PWA) 小图标：

| 操作 | 结果 |
| --- | --- |
| 左键单击托盘图标 | 恢复主窗口 |
| 右键托盘图标 → 显示主界面 / 退出 | 恢复窗口 / 真正结束进程 |
| Windows 托盘右键 → 打开管理界面 | 直接在浏览器打开管理页 |
| 再次从应用菜单 / 快捷方式启动 | 不启动第二个服务，直接唤起已有窗口 |

只有托盘菜单的「退出」、控制窗口的「退出」按钮、系统关机或 `Ctrl+C`（服务模式）
才结束进程。桌面环境若不显示系统托盘（如未装扩展的 GNOME），**再次启动本程序**
即可恢复窗口，程序自带单实例检测，不会重复开服务。Windows 的通知区域图标不可用时
会弹窗说明并退回「关窗即停止」，避免留下无法结束的后台进程。

### Windows 7 x64 / 8 / 10 / 11（仅 x64）

```bash
./packaging/windows/build-win.sh             # 在 Linux 上交叉编译 + 生成安装包
# 在目标 Windows 机器上运行 dist/PCWebADB-1.1.0-win7-setup.exe
```

也可免安装直接运行 `dist/PCWebADB-x64.exe`（64 位单文件）：
`adb.exe` 与两个 `AdbWin*.dll` 已内嵌进 exe，首次启动自动释放到用户目录，
**单独一个文件双击即可**，不再需要手动复制 adb（详见 `docs/WINDOWS.md` §3.1）。
NSIS 安装包安装的也是这个 x64 主程序，安装后文件名为 `PCWebADB.exe`。

Win7 x64 前提：**SP1 + KB2999226（UCRT，含 32 位运行库）**、以及 Chrome / Edge / Firefox 等现代浏览器
（IE 无法渲染本程序的前端）。详见 [docs/WINDOWS.md](docs/WINDOWS.md)。

## 功能

| 分组 | 页面 |
| --- | --- |
| 连接 | 设备连接（无线配对 / 手动连接 / 局域网发现 / 最近设备）、设备详情 |
| 管理 | 应用管理、文件管理、设备截图 |
| 调试 | 系统日志（快照 + 实时流）、ADB Shell、虚拟遥控器、屏幕镜像 |
| 系统 | 诊断日志、设置（端口 / 超时 / 网段 / 画质 / 主题 / 自定义 adb）、重启 ADB 服务、运行日志（`<配置目录>/logs/pcwebadb.log`，启动失败与 panic 也会落盘） |

内置官方 Android SDK Platform Tools：Linux 版为 **37.0.1**，Windows 版为
**34.0.4**（最后一个支持 Win7 的版本，34.0.5 起静态导入 Win8+ API）。
两者都支持 Android 11+ 无线调试配对；屏幕镜像调用系统 `scrcpy`（可选）。

## 目录

| 路径 | 说明 |
| --- | --- |
| `*.go` / `web/` | Go 后端与内嵌前端（纯标准库，无第三方模块）；平台实现按 build tag 分离 |
| `gui_gtk_linux.go` / `gui_gtk_stub.go` | Linux 原生 GTK3 + WebKitGTK 窗口与 GtkStatusIcon 托盘（cgo + `dlopen`）/ 降级空实现 |
| `tray_windows.go` / `tray_icon_linux.go` | Win32 `Shell_NotifyIconW` 托盘与单实例互斥 / Linux 内嵌托盘 PNG |
| `platform_unix.go` / `platform_windows.go` / `gui_windows.go` | Unix 窗口模式、flock 单实例与降级链 / Win32 原生启动器窗口 |
| `abi_windows_amd64.go` | Win32 结构体编译期 ABI 断言（`NOTIFYICONDATAW_V2_SIZE` 952） |
| `packaging/windows/make-icon.py` | 纯标准库图标生成器，产出 `.ico`（Win）与 `.png`（Linux 托盘） |
| `adb_bundle*.go` | Windows 内置 adb 三件套的 `go:embed` + 惰性释放（幂等、原子写、可自愈），含单元测试 |
| `reference/skyadb/` | 上游 GPL-3.0 源码快照 |
| `vendor/platform-tools/` | Linux 官方 adb 37.0.1 + NOTICE.txt |
| `vendor/platform-tools-win/` | Windows 官方 adb 34.0.4 + AdbWinApi.dll / AdbWinUsbApi.dll + NOTICE.txt |
| `debian/` | Debian 源包元数据（control/changelog/copyright/rules） |
| `packaging/` | 构建脚本、desktop、图标、man page、上游功能映射文档、PE 校验器 |
| `packaging/windows/` | NSIS 脚本与 Windows 构建脚本 |
| `docs/BUILD.md` | **Linux/.deb 完整构建说明**：仓库/协议、功能范围、架构与规范、依赖、流程、产物结构 |
| `docs/WINDOWS.md` | **Windows 7 版完整说明**：兼容边界、UCRT/浏览器前提、构建流程、产物结构、验证范围 |
| `dist/` | 最终 `.deb`、`exe`、`setup.exe`、源码归档与校验和 |

## 许可

- 本实现：**GPL-3.0-or-later**（`LICENSE`）；完整对应源码随 `.deb` 放在
  `/usr/share/doc/pcwebadb/source.tar.gz`，随 Windows 安装包放在
  `%PROGRAMFILES%\PC Web ADB (PWA)\source.tar.gz`。
- 参考项目：GPL-3.0，Copyright (C) 2026 github.com/sky22333。
- 内置 adb：Apache-2.0 + Android SDK License，见各 `vendor/*/NOTICE.txt`。
  它作为独立程序聚合分发，不受本项目 GPL 授权覆盖。
