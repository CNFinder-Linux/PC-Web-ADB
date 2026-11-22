# PC Web ADB (PWA) — 构建与打包说明

> 任务：制作 PC 端 ADB 工具软件，功能与交互参考手机上的 **sky adb (SkyADB)**；
> 先获取参考项目开源代码，完成编译打包，最终产出可安装的 `.deb`。
> 本文档明确目标仓库/协议、功能范围、目标架构与打包规范、构建依赖、编译流程、产物结构。
>
> **Windows / Windows 7 版本**见 [WINDOWS.md](WINDOWS.md)：同一份 Go 源码按 build tag
> 分离平台实现，用 Go 1.20.14 交叉编译出 PE32/PE32+ GUI 程序，并打包为 NSIS
> 简体中文安装程序 `PCWebADB-1.1.0-win7-setup.exe`。
>
> **2026-11-15 修订（Linux 版改为原生桌面应用）**：旧版是「启动回环 HTTP 服务 +
> `xdg-open` 打开浏览器标签页」，观感像一个网页服务。现改为**默认弹出 GTK3 原生
> 窗口，界面由内嵌 WebKitGTK 渲染**——独立标题栏、独立任务栏图标、无地址栏无标签页。
> GTK/WebKit 用 `dlopen` 运行时加载，缺库时自动降级为 Chromium/Firefox 的
> `--app` 独立窗口，再降级为纯服务模式。详见 §3.1。
>
> **2026-11-15 再修订（关窗转后台托盘）**：关闭窗口不再结束进程，窗口只是隐藏，
> `adb` 与回环服务继续运行，通知区域出现小图标（左键恢复 / 右键退出）；配合单实例
> 检测，再次启动会直接唤起已有窗口。Windows 7 版同步改为 `Shell_NotifyIconW`
> 托盘。实机截图见 `docs/screenshot-tray.png`，详见 §3.2。

---

## 1. 目标代码仓库与开源协议来源

| 项 | 内容 |
| --- | --- |
| 参考项目 | **sky adb**（Android 手机端 ADB 管理工具） |
| 仓库 | `https://github.com/sky22333/skyadb` |
| 分支 / 精确提交 | `main` @ `bbf01698880f3b09c40dd0672b297fe16fd70ef6`（2026-11-14） |
| 获取方式 | `https://codeload.github.com/sky22333/skyadb/zip/refs/heads/main`（已下载并快照） |
| 上游许可证 | **GPL-3.0**（`LICENSE` 首行 `Copyright (C) 2026 github.com/sky22333`） |
| 上游技术栈 | Kotlin / Jetpack Compose / MIUIX 0.9.4 / Kadb 2.1.5 / kadb-mdns / scrcpy-server 5.0.1 / DataStore |
| 本地快照 | `reference/skyadb/`（218 个文件，含 `LICENSE`、`README.md`、全部 Kotlin 源码与 Gradle 配置） |
| 本实现许可证 | **GPL-3.0-or-later**（功能层面的独立改编，保留上游署名；完整源码随包分发） |
| 内置 adb 许可证 | Android SDK Platform Tools 37.0.1，**Apache-2.0 + Android SDK License**（独立程序，聚合分发，`NOTICE.txt` 随包） |

### 为什么不是「直接编译上游源码」

上游是 **Android APK**：`./gradlew assembleRelease` 只能产出 `.apk`，依赖 Android SDK、
Android 运行时、Kadb（Kotlin 版 ADB 协议栈）与 scrcpy-server（Android MediaCodec）。
它既不能编译为 Linux ELF，也无法作为 Debian 包安装运行。因此本任务的处理是：

1. **先获取并固定上游开源代码**（已完整快照到 `reference/skyadb/`），从 `README.md`、
   `AdbManagerApp.kt`、`strings.xml`、各 `*Screen.kt` 中提取功能清单、页面结构与中文文案；
2. 用 **Go 1.20 + 内嵌 Web UI 独立实现**相同工作流的 Linux 桌面客户端；
3. **编译并打包这个实现**为 `.deb`。

二进制包中**不含**上游任何 Kotlin/Java 源码、Kadb、MIUIX、scrcpy-server 或 Android 资源；
上游源码仅作为设计参考与许可审计留存于源树。详见
`packaging/UPSTREAM-REFERENCE.md` 与 `debian/copyright`。

---

## 2. 需要保留 / 调整 / 移除的核心功能范围

### 2.1 保留（与上游一一对应）

| 上游功能 | 本实现 |
| --- | --- |
| 无线调试配对、连接、断开、最近设备记录 | 设备连接页；最近设备持久化到 `~/.config/pcwebadb/config.json` |
| 局域网自动发现、网段扫描 | 本机 /24 自动探测 + 自定义 CIDR（/16~/30）+ 并发 TCP 端口探测 + 自动 `adb connect` |
| 设备基础信息、连接状态 | `getprop`、`wm size/density`、`dumpsys battery`、`df`、`ip`、`uptime`、`getenforce` |
| 应用列表、分类筛选、搜索、启动、停止、卸载 | 用户/系统/全部筛选，包名搜索，启动 / 强停 / 卸载 / 冻结 / 启用 / 清数据路径 |
| 本地 APK 安装、在线下载 APK 后安装 | 本地文件选择 `adb install -r -d`；URL 下载后安装 |
| 目标设备文件管理、目录浏览、上传、下载 | `adb shell ls -lA` 解析、`adb push/pull`、新建 / 重命名 / 删除 |
| 截图预览，需要时再保存 | `adb exec-out screencap -p` 先预览，再下载 PNG |
| 系统日志查看与复制 | `logcat -d` 快照 + SSE 实时流 + 一键复制 |
| 模拟物理按键、虚拟遥控器 | 方向键/OK/返回/主页/多任务/菜单/音量/电源/媒体键 + `input text` |
| 屏幕镜像 | 屏幕镜像页（见 2.2 调整） |
| Shell 命令执行与输出查看 | Shell 页 + 常用命令预设 + 命令历史输出 |
| 诊断日志 | 内存环形缓冲 500 条，按级别/操作/时间展示 |
| 默认端口、连接超时、命令超时、镜像画质、主题 | 设置页全部保留 |

### 2.2 调整（平台差异）

| 项 | 上游（Android） | 本实现（PC） |
| --- | --- | --- |
| ADB 协议 | 进程内 Kadb 2.1.5 | 调用内置官方 `adb` 37.0.1（支持 `adb pair`；系统自带 1.0.39 不支持无线配对） |
| 设备发现 | kadb-mdns + `AdbProtocolProbe` | 有界并发 TCP 端口扫描，最多 4096 个地址，无需 mDNS 系统服务 |
| USB 连接 | USB OTG 主机 + Fastboot 协议栈 | 标准 PC USB 主机，`adb devices -l` 自动枚举 |
| 屏幕镜像 | 内嵌 scrcpy-server + MediaCodec 解码 | 调用系统 `scrcpy` 打开独立窗口（`Recommends: scrcpy`），未安装时明确提示 |
| 交互布局 | 底部 4 项导航 + 设备详情 7 个子页 | 左侧分组导航栏（连接/管理/调试/系统）+ 顶部设备选择器，页面划分保持一致 |
| 视觉 | MIUIX / Material 3 | Material 3 风格 CSS：卡片、圆角、状态徽标、深浅主题、Toast、确认弹窗 |
| 在线推送文件 | URL 下载后 push 到设备 | 保留 URL 安装 APK；通用文件推送改为本地选择上传 |
| 新增 PC 能力 | — | 扫描并发数、自定义扫描网段、默认文件目录、自定义 adb 路径、LAN 拒绝外部访问 |
| AI 助手 | OpenAI 兼容 / MCP / Koog | **不移植**（与 ADB 管理无关且需外部密钥） |

### 2.3 移除（Android 端特有）

- 本机用户应用导出并安装到目标设备（PC 上没有「本机 Android 应用」）
- USB OTG / Fastboot 设备摘要与命令
- 应用真实名称/图标解析（依赖 Android `PackageManager` 与 APK 资源解码）

### 2.4 安全边界（对齐上游校验器）

`validateRemoteCommand` / `validateRemotePath` 拒绝空值、控制字符、`..` 路径穿越，
禁止对 `/`、`/data`、`/system`、`/proc`、`/dev` 执行写操作；远端参数统一 `shq()`
单引号转义；包名、按键名、日志过滤表达式白名单校验；HTTP 服务仅监听
`127.0.0.1` 并校验 `Host` 头，阻断 DNS rebinding。

---

## 3. 目标 Debian 架构与打包规范

| 项 | 值 |
| --- | --- |
| 目标发行版 | Debian 系（Debian 10+/Ubuntu 18.04+/银河麒麟 V10 SP1/优麒麟等） |
| 目标架构 | **`amd64` (x86_64)**；源码可通过 `--arch arm64` 交叉编译（需自备 arm64 版 adb） |
| 包名 / 版本 | `pcwebadb` / `1.1.0-1` |
| 产物文件 | `dist/pcwebadb_1.1.0-1_amd64.deb` |
| deb 格式 | Debian 2.0（`debian-binary` + `control.tar.xz` + `data.tar.xz`），xz 压缩 |
| 控制字段 | `Package / Version / Section: utils / Priority: optional / Architecture / Maintainer / Homepage / Depends / Recommends / Suggests / Installed-Size / Description` |
| 依赖 | `Depends: xdg-utils`（降级路径用 `xdg-open`）；`Recommends: libgtk-3-0, libwebkit2gtk-4.0-37 \| libwebkit2gtk-4.1-0, scrcpy`；`Suggests: 浏览器、android-sdk-platform-tools` |
| 链接形态 | `CGO_ENABLED=1`，但 cgo 层**只调用 `dlopen`/`dlsym`**；二进制仅动态链接 libc / libdl / libpthread / libresolv（所有 Linux 自带），GTK/WebKit **不是链接期依赖**，缺库仍能启动 |
| 无硬依赖 | Go 运行时、GTK/WebKit 开发包、pkg-config、Qt、Node.js、Java/Android SDK、adb 系统包、任何第三方 Go module |
| 桌面集成 | `StartupWMClass=PCWebADB`、`SingleMainWindow=true`、`Terminal=false`；窗口由 `gtk_window_set_wmclass` 显式设置同名 WM_CLASS |
| 版权 | DEP-5 机器可读 `debian/copyright`，GPL-3 / Apache-2.0 引用 `/usr/share/common-licenses/*` |
| 维护脚本 | `postinst` / `postrm`：刷新 desktop database 与 hicolor 图标缓存（幂等、失败容错） |
| 完整性 | `DEBIAN/md5sums` 覆盖所有载荷文件；所有目录 0755、文件 0644、可执行文件 0755、owner root:root |
| 源码可得 | `usr/share/doc/pcwebadb/source.tar.gz`（GPL 对应源码）；上游来源说明见 `UPSTREAM-REFERENCE.md` |
| 桌面集成 | `usr/share/applications/pcwebadb.desktop`、hicolor SVG 图标、`pcwebadb(1)` man page |

`debian/` 目录同时提供 `control`、`changelog`、`copyright`、`rules`、`source/format: 3.0 (native)`，
`debian/rules` 为不依赖 debhelper 的独立 Makefile，可用 `dpkg-buildpackage -b -us -uc`；
**推荐的主构建路径**是 `packaging/build-deb.sh`（只依赖 `dpkg-deb`）。

### 3.1 Linux 桌面应用形态（本次修订重点）

| 层 | 实现 |
| --- | --- |
| 窗口 | `gui_gtk_linux.go`（`//go:build linux && cgo`）：GTK3 `GtkWindow` + `WebKitWebView`，1280×820，标题「PC Web ADB (PWA) · ADB 设备管理」，WM_CLASS `PCWebADB` |
| 库加载 | 全部符号经 `dlopen("libgtk-3.so.0")` / `dlopen("libwebkit2gtk-4.0.so.37" \| 4.1)` + `dlsym` 解析；**编译期不需要 GTK/WebKit 开发包或 pkg-config** |
| 生命周期 | `gtk_main()` 跑在进程初始 OS 线程（`runtime.LockOSThread`）；`delete-event` 回调返回 `TRUE` 并 `gtk_widget_hide()`，**窗口关闭但主循环继续**；只有托盘「退出」→ `sky_stop` → `gtk_main_quit` → 关闭 HTTP 服务才结束进程 |
| 托盘 | `GtkStatusIcon` + `GtkMenu`（同样由 `dlopen` 解析，无链接期依赖）；图标取自 `go:embed` 的 PNG，写入 `$XDG_CONFIG_HOME/pcwebadb/tray-icon.png` 后 `gtk_status_icon_set_from_file()`，未安装 `.deb` 时也能显示 |
| 跨线程 | GTK 只能由主线程调用。`g_timeout_add(200ms)` 轮询 C 侧的两个 `volatile` 标志：SIGINT/SIGTERM、`GET /api/raise` 与「第二次启动」都只置标志，绝不从 Go 协程直接调 GTK |
| 单实例 | `flock(LOCK_EX\|LOCK_NB)` 锁住 `instance.lock`；第二个 GUI 进程读 `instance.addr` → 请求既有实例的 `GET /api/raise` → 打印提示并退出，不会起第二个服务 |
| 内部通信 | 窗口与 Go 后端之间仍走 `127.0.0.1:<随机端口>`，但这是**进程内部传输**，不对外提供网页服务：仅回环、校验 Host 头、无监听地址配置、用户不可见 |
| 降级链 | `--window=auto`（默认）：GTK/WebKit 窗口 → Chromium/Edge `--app` 独立窗口（隔离 `--user-data-dir`，无地址栏/标签页）→ Firefox `--new-window` → `xdg-open` → 纯服务模式 |
| 显式模式 | `--window=gtk`（只用原生窗口）、`--window=browser`（只用独立应用窗口）、`--window=none` / `--headless` / `--no-browser`（无界面，供 SSH 转发或无图形会话）、`--new-instance`（跳过单实例检测） |
| 非 Linux / 无 CGO | `gui_gtk_stub.go` 提供空实现，自动走降级链；`--arch arm64` 因无 cgo 交叉编译器也走降级链 |

浏览器降级模式同样**不再把浏览器进程的生命周期与 Go 服务绑定**：独立应用窗口关闭后
服务继续在后台运行，`Ctrl+C` 才结束；此模式下 `GET /api/raise` 会重新拉起一个独立
应用窗口。`SKYADB_BROWSER` 可覆盖降级时使用的浏览器命令。

### 3.2 后台常驻与任务栏小图标（实机验证）

| 用户动作 | 结果 |
| --- | --- |
| 关闭 GTK 窗口（`wmctrl -ic` / 点 ×） | `delete-event` 返回 TRUE，窗口隐藏；进程存活，`/api/state` 仍 200 |
| 左键托盘图标 | `sky_show=1`，200 ms 内 `gtk_widget_show_all()` + `gtk_window_present()` |
| 右键 →「显示主界面」 | 同上 |
| 右键 →「退出 PC Web ADB (PWA)」 | `sky_stop=1` → `gtk_main_quit()` → 关闭 HTTP 服务 → 进程退出 |
| 再次从应用菜单启动 | `flock` 失败 → `GET /api/raise` → 唤起窗口 → 第二个进程立即退出 |
| `Ctrl+C` / `SIGTERM` | 与托盘退出等价（`--window=none` 模式下这是唯一退出方式） |

桌面环境不提供系统托盘（如未装 AppIndicator 扩展的 GNOME）时，图标可能不出现，
但「再次启动本程序」始终能恢复窗口，因此不会留下无法找回的后台进程。
`docs/screenshot-tray.png` 是在 UKUI 面板上的实机截图（蓝色「A」图标）。

---

## 4. 构建依赖

**构建期（必选）**

| 依赖 | 版本 / 用途 |
| --- | --- |
| Go | ≥ 1.20（本环境使用工作区自带 `../.toolchain/go/bin/go`，go1.20.14） |
| `gcc` + libc 头文件 | `libc6-dev` / `linux-libc-dev`；cgo 层仅编译一个 `dlopen` shim（不需要 GTK/WebKit 头文件） |
| `dpkg-deb` | Debian 打包（本机 dpkg 1.19.7） |
| GNU coreutils / findutils / tar / gzip / xz | `install`、`md5sum`、`find`、`tar --transform`、`gzip -9`、`dpkg-deb -Zxz` |

**构建期（不需要）**：GTK3 / WebKitGTK **开发包**、pkg-config、Qt、Node.js/npm、
Java/Android SDK、Gradle、debhelper、任何第三方 Go module（`go.mod` 只有 `go 1.20`，纯标准库）。
GTK/WebKit 只在运行时按需 `dlopen`。

**运行期**

- 必需：glibc 基础库（`libc` / `libdl` / `libpthread` / `libresolv`，所有 Linux 自带）。
- 原生窗口（默认）：X11/Wayland 图形会话 + `libgtk-3-0` + `libwebkit2gtk-4.0-37`
  （Debian 12+ 为 `libwebkit2gtk-4.1-0`，程序会自动尝试两种 soname）。二者通过
  `Recommends` 声明，**缺失不会导致程序无法启动**，会自动降级。
- 降级路径：`xdg-utils` 与 Chromium / Edge / Firefox 之一（打开独立 `--app` 窗口）。
- 可选：`scrcpy`（屏幕镜像）；USB 调试需 `adb` udev 规则。
- 包内自带 `adb` 37.0.1，因此无线配对开箱即用。

**环境注意**：
- 本机 `/tmp` 是 10 MB tmpfs，Go 编译的 `$WORK` 会写满；构建脚本已把
  `TMPDIR` / `GOCACHE` 指向项目内 `.tmp-go/`。若自行编译请显式设置这两个变量。
- 本机沙箱精简掉了 `/usr/include/stdlib.h`。`packaging/build-deb.sh` 会自动调用
  `packaging/prepare-sysroot.py`，用 `apt-get download libc6-dev linux-libc-dev`
  （免 root）解包到项目内 `.sysroot/`，并修正 `libc.so` 链接脚本中的绝对路径；
  正常 Debian 构建机检测到系统头文件后完全跳过这一步。`.sysroot/` 不进入源码包。

---

## 5. 编译流程

```bash
cd /home/finder/WorkBuddy/WB-work/skyadb-pc

# 一键：编译 + 组装 + 打包 + 校验
./packaging/build-deb.sh

# 指定 Go / 交叉编译（arm64 需提供对应架构的 adb）
./packaging/build-deb.sh --go /usr/bin/go --arch amd64
ADB_BIN=/path/to/arm64/adb ./packaging/build-deb.sh --arch arm64
```

脚本内部 7 个阶段：

1. **定位 Go 工具链** → `go env GOROOT`，设置 `GOOS=linux GOARCH=amd64`、
   `TMPDIR=.tmp-go/work`、`GOCACHE=.tmp-go/cache`。
2. **从 `debian/changelog` 首行解析版本**（当前 `1.1.0-1`），确定产物文件名。
3. **准备原生窗口构建环境**：`amd64` 时 `CGO_ENABLED=1` 并检查 `gcc`；若系统缺
   `/usr/include/stdlib.h`，调用 `prepare-sysroot.py` 在项目内准备 `.sysroot/` 并
   注入 `CGO_CFLAGS` / `CGO_LDFLAGS`。非 amd64 交叉编译则 `CGO_ENABLED=0`（走降级链）。
4. **编译**：`go build -trimpath -ldflags="-s -w" -o dist/pcwebadb .`
   （`//go:embed all:web` 把前端编进二进制；`gui_gtk_linux.go` 的 cgo shim 只含
   `dlopen`/`dlsym`），随后 `--version` 自检。
5. **组装 staging** `packaging/root/`：安装二进制、内置 adb、Web 副本、desktop、SVG、
   man page、copyright、changelog.gz、README.Debian、UPSTREAM-REFERENCE.md、
   NOTICE.txt、ADB-VERSION.txt、`source.tar.gz`（排除 `.sysroot/`）；统一权限；生成
   `DEBIAN/control`（从 `debian/control` 的 Package 段提取并注入 `Version`、
   实际 `Installed-Size`）、`md5sums`、`postinst`、`postrm`。
6. **打包**：`dpkg-deb -Zxz --build --root-owner-group packaging/root dist/<name>.deb`。
7. **校验**：`dpkg-deb -f/-c`、`ar t`，并用 Python `tarfile` 只读比对关键载荷的
   SHA-256、权限与 owner（不使用 `dpkg-deb -x`，避免在受限沙箱里因 `utime()` 被禁而误报）。

等价的纯手工流程：

```bash
export GOROOT=$(go env GOROOT); export CGO_ENABLED=1 GOOS=linux GOARCH=amd64
export TMPDIR=$PWD/.tmp-go/work GOCACHE=$PWD/.tmp-go/cache
# 仅在系统缺 libc 头文件时：
# python3 packaging/prepare-sysroot.py "$PWD/.sysroot"
# export CGO_CFLAGS="-I$PWD/.sysroot/usr/include -I$PWD/.sysroot/usr/include/x86_64-linux-gnu"
# export CGO_LDFLAGS="-B$PWD/.sysroot/usr/lib/x86_64-linux-gnu -L$PWD/.sysroot/usr/lib/x86_64-linux-gnu -L/usr/lib/x86_64-linux-gnu"
go build -trimpath -ldflags="-s -w" -o dist/pcwebadb .
# 然后按第 5 步组装 staging，再 dpkg-deb --build
```

Debian 源包路径（可选）：`dpkg-buildpackage -b -us -uc`（`debian/rules` 会复用同一脚本，
并把 `.deb` 复制到上层目录）。

---

## 6. 最终产物应包含的文件结构

### 6.1 交付的 `.deb` 载荷

```
./
├── usr/bin/pcwebadb                          0755  Go 主程序（内嵌 Web UI + GTK/WebKit 运行时窗口）
├── usr/lib/pcwebadb/adb                     0755  Android SDK Platform Tools adb 37.0.1
├── usr/share/pcwebadb/web/
│   ├── index.html                                 前端源码副本（便于审计；运行时用内嵌副本）
│   ├── app.css
│   └── app.js
├── usr/share/applications/pcwebadb.desktop       应用菜单入口
├── usr/share/icons/hicolor/scalable/apps/pcwebadb.svg
├── usr/share/man/man1/pcwebadb.1.gz             man page
└── usr/share/doc/pcwebadb/
    ├── copyright                                 DEP-5 版权与许可证
    ├── changelog.Debian.gz
    ├── README.Debian                             安装/使用/adb 版本冲突说明
    ├── UPSTREAM-REFERENCE.md                     仓库、提交、协议、功能映射
    ├── NOTICE.txt                                内置 adb 的第三方许可声明
    ├── ADB-VERSION.txt                           Pkg.Revision=37.0.1
    └── source.tar.gz                             GPL 对应源码（Go + Web + 打包脚本 + 文档）

DEBIAN/
├── control                                      包元数据
├── md5sums                                      载荷校验和
├── postinst                                     刷新桌面/图标缓存
└── postrm
```

### 6.2 源工程结构（供二次构建）

```
pcwebadb/
├── go.mod                          module github.com/workbuddy/pcwebadb, go 1.20
├── main.go                         入口：adb 定位、单实例、回环内部服务、窗口模式与 /api/raise
├── config.go                       设置 / 最近设备持久化、诊断日志环形缓冲
├── adb.go                          adb 封装、设备/信息/应用/文件/截图/按键、路径与命令校验
├── discovery.go                    局域网网段枚举、CIDR 展开、并发端口探测
├── api.go                          REST/SSE 接口、上传下载、scrcpy 进程管理
├── gui_gtk_linux.go                ★ Linux 原生 GTK3 + WebKitGTK 窗口与托盘（cgo + dlopen）
├── gui_gtk_stub.go                 非 Linux / 无 CGO 时的降级空实现
├── tray_icon_linux.go              Linux 托盘图标 PNG 的 go:embed
├── tray_windows.go                 Win32 通知区域托盘、菜单、单实例互斥、ICO→HICON、TaskbarCreated 重建
├── applog.go                       运行日志（<配置目录>/logs/pcwebadb.log）、panic 报告、诊断落盘
├── platform_unix.go                Unix 启动流程、flock 单实例、原生窗口 → 独立应用窗口 → 纯服务
├── web/{index.html,app.css,app.js} 内嵌前端（11 个页面）
├── reference/skyadb/               上游 GPL-3.0 源码快照（不进入 .deb）
├── vendor/platform-tools/          官方 adb + NOTICE.txt + source.properties
├── debian/{control,changelog,copyright,rules,source/format}
├── packaging/{build-deb.sh,prepare-sysroot.py,*.desktop,*.svg,*.png,*.1,README.Debian,UPSTREAM-REFERENCE.md,check_pe.py}
├── packaging/windows/{build-win.sh,installer.nsi,versioninfo.rc,make-icon.py,pcwebadb.ico}
│       versioninfo.rc 经 windres 生成 .syso，为 exe 注入文件图标与 VERSIONINFO
├── docs/{BUILD.md,WINDOWS.md,screenshot-linux.png,screenshot-tray.png}
├── LICENSE                         GPL-3.0 全文 + 本项目版权声明
├── README.md
└── dist/pcwebadb_1.1.0-1_amd64.deb   ★ 最终交付物
```

---

## 7. 验证结果（本次构建）

| 检查 | 结果 |
| --- | --- |
| `go vet`（linux/amd64、windows/amd64） | ✅ 均通过 |
| `go build`（CGO_ENABLED=1 + dlopen shim） | ✅ amd64 ELF、stripped；`ldd` 只有 libc / libdl / libpthread / libresolv，**无 GTK/WebKit 链接期依赖** |
| `pcwebadb --version` | ✅ `PC Web ADB (PWA) 1.1.0 (reference skyadb@bbf0169)` |
| **X11 实机：原生 GTK 窗口** | ✅ `wmctrl -lx` 显示 1280×820 独立窗口「PC Web ADB (PWA) · ADB 设备管理」，`_NET_WM_PID` 与进程一致 |
| **WebView 真实渲染** | ✅ 窗口截图确认中文界面、左侧分组导航、卡片与按钮完整；`WebKitWebProcess`/`WebKitNetworkProcess` 启动，`ss` 显示它们确实连上回环端口 |
| **WM_CLASS / 桌面集成** | ✅ `PCWebADB`，与 `StartupWMClass=PCWebADB` 一致 |
| **关闭窗口 → 后台常驻** | ✅ X11 实机：`wmctrl -ic` 后窗口隐藏、进程存活、`/api/state` 仍 200、非法 Host 仍 403；`GET /api/raise` 与第二次启动都能恢复窗口；SIGTERM 后进程退出且端口关闭 |
| **托盘小图标** | ✅ UKUI 面板通知区域出现蓝色「A」图标（`docs/screenshot-tray.png`）；左键恢复 / 右键退出由 `g_timeout_add` 轮询驱动 |
| 降级链（`--window=none` / `--headless`） | ✅ 无界面服务正常启动，API 可用 |
| 本地服务 `/`, `/app.css`, `/app.js` | ✅ 200 / 15972 / 13593 / 30460 字节 |
| `/api/state`、`/api/devices`、`/api/settings`、`/api/diagnostics` | ✅ 正常返回 JSON |
| 无设备时错误路径（apps/files/screenshot/logcat/info/shell/key） | ✅ 均返回结构化错误，无 panic |
| 非法主机 / 非法按键 / 非法路径 | ✅ 被校验器拒绝 |
| `/api/discover`（127.0.0.0/30 小网段） | ✅ 正常完成，返回空结果 |
| `Host: evil.example` | ✅ 403（Host 头防护生效） |
| `dpkg-deb -I` / `-c` / `-f` | ✅ 控制字段、载荷、md5sums、维护脚本齐全 |
| 载荷 SHA-256 / mode / uid:gid | ✅ 4 个关键文件比对通过，root:root，0755/0644 |
| 解包后的 `usr/bin/pcwebadb` + `usr/lib/pcwebadb/adb` 联合运行 | ✅ 服务启动、`/api/state` 识别 adb 37.0.1、页面可访问 |

> 本机为沙箱环境：`/tmp` 仅 10 MB、非 root、`utime()` 被禁，因此
> `dpkg -i` 与 `dpkg-deb -x` 会因「无法写 /var/log/dpkg.log」和
> 「tar: 无法 utime」报错。这些是**环境限制，不是包缺陷**——载荷文件实际已完整解出、
> 权限正确、两个二进制均可执行。在普通 Debian/Ubuntu/Kylin 主机上以 root 执行
> `sudo dpkg -i pcwebadb_1.1.0-1_amd64.deb` 即可正常安装。

---

## 8. 安装与使用

```bash
sudo dpkg -i dist/pcwebadb_1.1.0-1_amd64.deb
sudo apt-get -f install            # 补齐 libgtk-3-0 / libwebkit2gtk-4.0-37 等推荐依赖
# 从应用菜单点「PC Web ADB (PWA)」，或：
pcwebadb                          # 打开原生 GTK/WebKit 应用窗口；关窗转入后台托盘
pcwebadb --window=gtk             # 强制原生窗口
pcwebadb --window=browser         # 缺 WebKit 时：独立 --app 窗口
pcwebadb --headless --port 18888  # 无图形会话 / 纯服务
pcwebadb --version
man pcwebadb
sudo dpkg -r pcwebadb
```

连接流程：目标设备开启「开发者选项 → 无线调试」→ 首次用「无线配对」输入 6 位配对码
→ 在「设备连接」输入 IP:端口点击连接（或用「局域网发现」扫描）→ 顶部选择设备
→ 左侧导航进入应用/文件/截图/日志/Shell/遥控器/镜像等页面。

配置：`~/.config/pcwebadb/config.json`（0600）；临时上传/下载文件在
`$TMPDIR/pcwebadb/`（0700，用完即删）；诊断日志只在内存中。
