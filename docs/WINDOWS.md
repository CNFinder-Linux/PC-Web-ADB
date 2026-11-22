# PC Web ADB (PWA) — Windows 7 版本说明

本文档说明 Windows 版（仅 64 位，含 Windows 7 SP1 x64）的兼容性边界、运行前提、构建依赖与流程、安装包结构以及验证范围。Linux/Debian 版见 [BUILD.md](BUILD.md)。

---

## 1. 兼容性结论

| 项目 | 结论 |
| --- | --- |
| 支持系统 | **Windows 7 SP1 x64、Windows 8/8.1 x64、Windows 10/11 x64、Server 2008 R2+** |
| 主交付 | 64 位 `PCWebADB-x64.exe`（PE32+ GUI）；安装包内名为 `PCWebADB.exe` |
| 其他 | 不再提供 x86 构建；32 位 adb 作为内置组件在 WOW64 下运行 |
| 编译器 | **Go 1.20.14**（Go 1.21 起官方要求 Windows 10，Win7 必须锁 1.20.x） |
| 运行时依赖 | 仅 Win7 自带 DLL（`kernel32.dll` 等）；**不需要** .NET、VC++ 运行库本体（见 §2）、WebView2、UWP、Java、Node、Python |
| 内置 adb | **Android SDK Platform Tools 34.0.4**（最后一个支持 Win7 的版本，见 §2） |

PE 校验结果（`packaging/check_pe.py`，只读解析 DOS/NT 头、导入表与函数名）：

| 二进制 | OS/Subsystem 版本 | 静态导入 | Win8+/10 标记 | 结论 |
| --- | --- | --- | --- | --- |
| `PCWebADB-x64.exe` | 6.1 / 6.1 | `kernel32.dll` | 无 | PASS |
| `adb.exe` 34.0.4 | 6.0 / 6.0 | Win7 DLL + `api-ms-win-crt-*` | `SetThreadDescription`（仅动态解析） | PASS（需 UCRT） |
| `AdbWinApi.dll` / `AdbWinUsbApi.dll` | 6.1 / 6.0 | Win7 DLL | 无 | PASS |
| `adb.exe` 37.0.1（已弃用） | 6.0 / 6.0 | +`api-ms-win-core-synch-l1-2-0.dll` | **静态导入 `GetSystemTimePreciseAsFileTime`** | **FAIL** |

`GetSystemTimePreciseAsFileTime` 是 Windows 8 才加入的 API，37.0.1 在 Win7 上加载阶段就会失败；这是选择 34.0.4 而不是最新版的可验证依据（与 Google issue tracker「34.0.5 起不再支持 Win7」的记载一致）。

---

## 2. 两个必须说明的运行前提

### 2.1 通用 C 运行库（UCRT）

Google 用 MSVC 编译的 `adb.exe` 静态导入 `api-ms-win-crt-*.dll`。这些 **不是 Windows 10 专属**：Windows 7 SP1 安装 **KB2999226**（或 Microsoft Visual C++ 2015–2019 Redistributable）后即具备。所有现代官方 platform-tools（含 30.0.5、34.0.4、37.0.1）都依赖它，因此无法通过降级 adb 规避。内置的 34.0.4 `adb.exe` 是 **32 位 PE**，在 Win7 x64 上还需要 **32 位 UCRT**（`SysWOW64\ucrtbase.dll`）。

- NSIS 安装程序在 `.onInit` 先检测 `$WINDIR\SysWOW64\ucrtbase.dll`，再回退检测 `$SYSDIR`，缺失时弹出明确提示（不阻断安装）。
- 本程序自身（Go 静态编译）**不需要** UCRT，缺少 UCRT 只影响 adb 相关功能。

### 2.2 现代浏览器

管理界面是内嵌在 exe 里的 Web UI，由浏览器渲染。Windows 7 自带的 IE 8/11 **无法正确渲染**（前端使用 ES6 / CSS 变量 / Fetch）。启动器会按以下顺序探测并使用已安装的浏览器：

Chrome、Edge、Firefox、Chrome Canary、Brave、Vivaldi、Opera、360 浏览器、QQ 浏览器、搜狗浏览器 → PATH 中的 `msedge.exe`/`chrome.exe`/`firefox.exe` → `ShellExecuteW("open")` 交给系统默认关联。

若没有现代浏览器，控制窗口会显示提示，可复制地址到任意现代浏览器打开。**除浏览器外无其他外部运行依赖**；USB 调试还需设备厂商的 USB 驱动（与任何 ADB 工具相同）。

---

## 3. 架构与 Win7 适配要点

```
PCWebADB.exe (PE32, GUI subsystem, Go 1.20.14, CGO_ENABLED=0)
├── go:embed web/          内嵌 Material 风格管理界面（无外部静态文件）
├── go:embed vendor/...adb 内嵌 adb.exe 34.0.4 + AdbWinApi.dll + AdbWinUsbApi.dll
├── net/http 127.0.0.1    本地服务，Host 头校验，仅监听回环
├── Win32 启动器窗口        user32/gdi32/kernel32/shell32 原生控件
│   ├── 地址 / adb 路径 / 状态文本
│   └── 「打开界面」「复制地址」「退出」按钮；关闭窗口 = 隐藏到托盘
├── 通知区域图标            Shell_NotifyIconW + 右键菜单（后台常驻）
│   ├── 左键：显示控制窗口
│   └── 右键：打开管理界面 / 显示控制窗口 / 退出（唯一彻底结束途径）
├── 单实例互斥              Local\PCWebADB_SingleInstance + FindWindowW/PostMessageW
├── 子进程 adb.exe         CREATE_NO_WINDOW，避免每次调用闪黑框
└── 配置                  %APPDATA%\PC Web ADB\config.json
```

关键设计：

- **不用浏览器控件**：WebView2（Win10+）、UWP、.NET 4.6+、MSHTML/IE 内核全部排除；Win32 原生控件在 Win7 上零依赖。
- **内置 adb，单文件可运行**：`adb.exe` 与两个 WinUSB DLL 直接编进 exe，首次运行按需释放到用户目录；单独拷贝 `PCWebADB.exe` 双击即可，不再要求同目录有 adb（详见 §3.1）。
- **`-H windowsgui`**：PE Subsystem=2，不弹控制台；因此用原生窗口提供可见的启动/退出界面，替代 Linux 版的 Ctrl+C 循环。
- **关窗不停服务**：`WM_CLOSE` 不再 `DestroyWindow`，改为 `ShowWindow(SW_HIDE)`；`WM_QUERYENDSESSION` 只返回 TRUE 表示同意关机，真正的清理放在 `WM_ENDSESSION`（`wParam != 0`）。托盘菜单 / 「退出」按钮调用 `quitApp()` → 删除图标 → `DestroyWindow` → `WM_DESTROY` → `PostQuitMessage` → 消息循环结束 → 关闭 HTTP 服务。
- **托盘只用 Win7 自带 API**：`Shell_NotifyIconW`（V2 结构）+ `CreatePopupMenu` / `AppendMenuW` / `TrackPopupMenu` + `CreateIconFromResourceEx`。图标由 `go:embed` 的 `.ico` 在运行时转成 `HICON`，**不需要** `.res` 资源、windres、COM、manifest 或第三方的托盘库；Explorer 尚未就绪时 `NIM_ADD` 会重试 4 次；即使初次失败，收到 `TaskbarCreated` 后仍会重新添加图标。
- **托盘失败有兜底**：若通知区域完全不可用，弹窗说明并恢复「关窗即停止服务」，避免留下一个既看不见又无法结束的后台进程。
- **单实例**：`CreateMutexW(Local\PCWebADB_SingleInstance)`；第二次启动在检测到互斥体后最多等待 2 秒反复 `FindWindowW`，找到主窗口就 `PostMessageW(WM_APP+2)` 唤起并退出，避免“互斥体已建、窗口还没建”的竞态开出第二个 adb 服务。`GET /api/raise` 走同一条路径。
- **结构体 ABI 断言**：`abi_windows_amd64.go` 用编译期数组长度断言锁定 `WNDCLASSEXW` 80、`MSG` 48、`POINT` 8、`NOTIFYICONDATAW_V2_SIZE` **952**（用 mingw-w64 `shellapi.h` 实测确认）；`MSG.message` 用 `uintptr` 填充 amd64 对齐槽。尺寸不符则编译失败。
- **窗口回调**：`syscall.NewCallback` 的 `wndProc` 带 `defer recover()`，回调内 panic 不会杀进程。
- **`CREATE_NO_WINDOW`**：`platform_windows.go:tuneCommand()` 给所有 `adb.exe` / `scrcpy.exe` 子进程设置该标志；浏览器等 GUI 子进程不受影响。
- **配置路径**：Windows 走 `%APPDATA%\PC Web ADB`（Unix 走 `$XDG_CONFIG_HOME/pcwebadb`），由 build tag 分离。
- **剪贴板**：`GlobalAlloc/GlobalLock` + `CF_UNICODETEXT` 原生实现，无第三方库。

功能范围与 Linux 版完全一致，参见 `UPSTREAM-REFERENCE.md`：设备发现、无线配对/连接/断开、最近设备、设备信息、应用管理与 APK 安装、文件双向传输、截图、Logcat（快照 + 实时流）、Shell、虚拟遥控器、诊断日志、设置；投屏依赖外部 `scrcpy`（可选）。

### 3.1 内置 adb 与「单文件直接运行」

早期版本把 `adb.exe` 只放在安装包里，于是**单独拷贝 `PCWebADB.exe` 双击时会弹出「未找到 adb.exe」**，而且提示还要求用户去敲 `--adb` 命令行参数——GUI 子系统程序没有控制台，这条指引根本不可执行。现已改为：

```
go:embed vendor/platform-tools-win/{adb.exe,AdbWinApi.dll,AdbWinUsbApi.dll}
        │
        └─ 启动时按以下优先级定位 adb（resolveAdb）：
             1. --adb 显式指定
             2. %SKYADB_ADB% 环境变量
             3. 设置中保存的 adbPath
             4. exe 同目录 / bin / platform-tools 等（安装版命中这里）
             5. 项目开发目录的相对路径
             6. 【惰性】释放内嵌副本 ← 仅前 5 项都找不到时执行
             7. PATH 上的 adb（最后兜底，可能是老版本）
```

释放位置随系统环境依次尝试 `%LOCALAPPDATA%`、`%APPDATA%`、`%USERPROFILE%\AppData\Local`、`%TEMP%` 下的
`PC Web ADB (PWA)\adb\34.0.4\`。目录名带版本号，避免升级后新旧 `adb.exe` / `AdbWinApi.dll` 混用（这类版本错配会导致 `adb server didn't ACK`）。三个文件必须同目录，因为 Windows 按 **adb.exe 所在目录** 解析 DLL。

实现要点（`adb_bundle.go` / `adb_bundle_windows.go`）：

- **惰性释放**：安装版同目录已有 adb，完全不会触发写入；只有单文件运行等场景才释放。
- **幂等且可自愈**：已有文件逐字节校验，一致则跳过；内容损坏、大小相同但字节不同、或只释放了一半，下次启动都会补齐。
- **原子写**：先写 `.<pid>.tmp` 再 `Remove` + `Rename`，崩溃或多个实例并发不会留下半个 `adb.exe`。
- **不需要管理员权限**：写用户目录而非 `%ProgramFiles%`，标准用户、免安装场景都可用。
- **可诊断**：启动时会把 adb 路径与版本写入「诊断日志」；若检测到缺少 `ucrtbase.dll` 或 adb 根本无法执行，会额外写入带处理办法的警告/错误条目。找不到 adb 时的弹窗也已改成可操作的中文指引。

代价：x64 exe 增大约 5.8 MiB（内含完整 adb），源码归档因此必须包含 `vendor/platform-tools-win/`——这些文件现在是 `go:embed` 的编译输入，构建脚本会校验归档中确实存在。

### 3.2 后台常驻与通知区域图标

旧版把「关闭控制窗口」等同于「结束进程」，浏览器里已打开的管理页面立刻变成连接失败。现在的行为：

| 用户动作 | 结果 |
| --- | --- |
| 点窗口右上角 × / Alt+F4 | 窗口隐藏，**服务和 adb 继续后台运行** |
| 左键单击通知区域小图标 | 恢复控制窗口 |
| 右键 → 打开管理界面 | 用浏览器打开管理页 |
| 右键 → 显示控制窗口 | 恢复控制窗口 |
| 右键 → 退出 PC Web ADB (PWA) / 窗口「退出」按钮 | 删除托盘图标、停止服务、结束进程 |
| 再次双击 exe / 桌面快捷方式 | 唤起已有窗口，不新增进程 |
| 系统注销 / 关机 | `WM_QUERYENDSESSION` 触发完整退出 |

第一次隐藏时会弹一次气泡提示，说明「服务继续运行、右键可退出」。托盘图标来自
`packaging/windows/pcwebadb.ico`（`make-icon.py` 纯标准库生成，含 16×16 / 32×32
两个 32bpp 图像），由 `go:embed` 编进 exe，运行时用
`CreateIconFromResourceEx()` 解码；窗口图标、Alt-Tab 图标也复用同一个 `HICON`。
`NOTIFYICONDATAW` 采用 V2 布局（`cbSize` = 936 / 952），不依赖 Vista+ 专属字段，
Win7 的 `explorer.exe` 可直接识别。

单实例由命名互斥体实现；`GET /api/raise`（仅回环、受 Host 头校验保护）只是向主窗口
`PostMessage`，不会从 HTTP 线程直接调用任何 GUI API。

---

## 4. 构建依赖

| 依赖 | 版本 / 用途 | 备注 |
| --- | --- | --- |
| Go | **1.20.14**（`linux-amd64` 宿主交叉编译） | 唯一编译器；`GOOS=windows GOARCH=amd64`，无 cgo、无第三方 Go 模块 |
| NSIS | 工作区私有免 root 工具链 `putty-cn/.tools/root`（`makensis` 3.x + `MUI2.nsh` + `SimpChinese.nsh`） | 可用 `NSIS_TOOLS=` 覆盖；需 `NSISDIR` 与 `LD_LIBRARY_PATH` |
| Python | 3.13（标准库） | `check_pe.py`、UTF-8 BOM 转换、预处理校验 |
| GNU coreutils / tar / file / sha256sum | 系统自带 | 源码归档、校验和 |
| platform-tools | 官方 `platform-tools_r34.0.4-windows.zip` 中的 `adb.exe` / `AdbWinApi.dll` / `AdbWinUsbApi.dll` / `NOTICE.txt` / `source.properties` | 已固化在 `vendor/platform-tools-win/`，构建不联网 |
| 上游参考 | `github.com/sky22333/skyadb` @ `bbf0169`（GPL-3.0） | 快照在 `reference/skyadb/`，仅作规格与署名依据 |

环境限制：`/tmp` 仅 10 MB tmpfs，脚本把 `TMPDIR` 与 `GOCACHE` 指向项目内 `.tmp-go/`。全程无需 root、无需系统安装、无需联网（若有本地工具链）。

---

## 5. 构建流程

```bash
cd pcwebadb
./packaging/windows/build-win.sh
```

七个步骤：

1. 校验 Go 版本必须是 1.20.x，检查 `makensis` / `MUI2.nsh`。
2. `GOOS=windows GOARCH=amd64 go vet` + 单元测试（捕获 build tag / ABI 类型错误）。
3. 交叉编译单个 x64 exe：`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H windowsgui"`，并链入 windres 生成的 `.rsrc` 图标/VERSIONINFO。
4. 用 `check_pe.py` 校验 x64 exe（`--require-gui`）、adb 与两个 DLL；断言 `Pkg.Revision=34.0.4`。
5. 生成 `pcwebadb-1.1.0-source.tar.gz`（全部 `.go`、`go.mod`、`web/`、`packaging/`、`docs/`、`debian/`、`LICENSE`、`README.md`），作为 GPL-3.0 对应源码随包分发；与 `.deb` 内嵌源码归档字节一致。
6. 把 `installer.nsi` 转成 UTF-8 BOM 副本，先 `-PPO` 预处理验证中文被正确读入，再用 `-INPUTCHARSET UTF8 -DROOT=<项目根>` 编译 NSIS 安装包；最后校验安装包 stub 的 PE 头。
7. 生成 `dist/SHA256SUMS-windows.txt`。

Linux `.deb` 仍由 `./packaging/build-deb.sh` 构建（Go 源码已按 build tag 分离，两者共用同一份源码）。

---

## 6. 产物与安装后文件结构

`dist/`：

```
PCWebADB-x64.exe                    64 位主程序（单文件，已内嵌 adb；同时进安装包）
PCWebADB-1.1.0-win7-setup.exe       NSIS 简体中文安装程序（仅 x64）
pcwebadb-1.1.0-source.tar.gz       GPL-3.0 对应源码（含 go:embed 输入 vendor/platform-tools-win/）
SHA256SUMS-windows.txt              校验和
```

**免安装用法**：直接把 `PCWebADB-x64.exe` 单独拷到任意目录双击即可，不需要再手动复制 `adb.exe` 和两个 DLL；程序首次启动会把它们释放到 `%LOCALAPPDATA%\PC Web ADB\adb\34.0.4\`。若该目录不可写（受限账户 / 安全软件拦截），再把 `vendor/platform-tools-win/` 里的三个文件放到 exe 同目录作为替代。

安装到 `%PROGRAMFILES64%\PC Web ADB (PWA)\`：

```
PCWebADB.exe                       64 位主程序（已内嵌 adb；开始菜单/桌面快捷方式指向它）
platform-tools.properties          内嵌 adb 的版本溯源（34.0.4）
LICENSE.txt                        GPL-3.0 全文 + 项目版权与聚合声明
ADB-NOTICE.txt                     Android SDK Platform Tools 许可与 NOTICE
README.md / WINDOWS.md             说明
UPSTREAM-REFERENCE.md              上游仓库、协议与功能映射
Uninstall.exe                      卸载程序
```

GPL-3.0 对应源码为 `dist/pcwebadb-1.1.0-source.tar.gz`（也在 Linux `.deb` 的
`/usr/share/doc/pcwebadb/source.tar.gz` 中），不塞进 NSIS 安装包，以免该工具链的
数据块上限触发 `SIGBUS`。

`adb.exe`、`AdbWinApi.dll`、`AdbWinUsbApi.dll` **不再作为独立文件安装**：它们已编译进 `PCWebADB.exe`（源文件即 `dist/PCWebADB-x64.exe`），程序按需释放到 `%LOCALAPPDATA%\PC Web ADB\adb\34.0.4\`。安装包只放一份 exe，避免重复安装 6 MiB 的相同 adb 数据。内置的 platform-tools 34.0.4 `adb.exe` 是 32 位程序，在 Win7 x64 上通过 WOW64 运行，因此仍需 32 位 UCRT。

注册表：`HKLM\Software\PC Web ADB\InstallDir`、`HKLM\...\Uninstall\PCWebADB`（DisplayName / UninstallString 等）；开始菜单与可选桌面快捷方式；卸载保留 `%APPDATA%\PC Web ADB\` 中的用户设置。

---

## 7. 命令行参数

```
PCWebADB.exe                 启动控制窗口并自动打开管理界面
PCWebADB.exe --no-browser    只启动服务，在窗口中显示地址
PCWebADB.exe --port 18765    固定监听端口（默认自动选空闲端口）
PCWebADB.exe --adb <path>    使用指定 adb.exe
PCWebADB.exe --new-instance  跳过单实例检测，另起一个独立服务（调试用）
PCWebADB.exe --version       输出版本（GUI 子系统，需重定向到文件查看）
```

也可用环境变量 `SKYADB_ADB` 指定 adb。服务仅监听 `127.0.0.1`，并校验 Host 头，不接受外部访问。
默认界面模式启用单实例；`--no-browser` / `--headless` / `--window=none` 不启用，便于多开服务。

---

## 8. 验证范围（诚实声明）

**已在本机验证：**

- Go 1.20.14 交叉编译 windows/amd64 成功；`go vet`（windows/amd64）通过；`file` 确认为 PE32+ GUI。
- `check_pe.py` 对 x64 exe、adb 34.0.4、两个 DLL、安装包 stub 的 PE 头 / 导入表 / 函数名校验全部 PASS；37.0.1 对照 FAIL。
- 编译期 Win32 结构体 ABI 断言在 amd64 下通过（`NOTIFYICONDATAW_V2_SIZE`=952，尺寸不符会直接编译失败）。
- NSIS 预处理输出确认简体中文字符串被正确解码；安装包可生成且 stub 为 Win7 兼容 PE32 GUI（OS/Subsystem 4.0，仅导入 Win7 DLL）。
- 安装包 `7z l` 列出全部载荷成员，`7z x` 实际解包后，主程序、许可 / NOTICE、说明文档与版本溯源共 **7 个文件与源文件 SHA-256 逐一一致**；构建脚本每次复验该清单，并断言载荷中不含重复的 `adb.exe` / 两个 DLL / x86 exe。GPL 源码归档在 `dist/` 与 `.deb` 中单独提供。
- 本机 NSIS 3.x 的 `makensis` 在输入总量超过约 16 MiB 时稳定崩溃（`SIGBUS`，与压缩器、datablock 去重、临时目录均无关，已用极简脚本二分验证）。因此安装包只放一份已内嵌 adb 的 x64 主程序；同一文件在 `dist/` 作为免安装单文件版提供。
- x64 exe 的字节流中确认存在内嵌前端内容（`设备连接`、`/api/devices`、`app.js`），`go:embed` 生效。
- **内置 adb 修复的专项验证**：`go vet` + 单元测试（`TestWriteBundle*`：创建全部文件、幂等且修复同长度损坏、部分释放后自愈、不可写目录报错）全部 PASS；构建脚本在每次编译时用 `bytes.find` 在 x64 exe 中精确查找 `adb.exe`、`AdbWinApi.dll`、`AdbWinUsbApi.dll` 的完整字节序列，并比对源文件 SHA-256，三者均命中；源码归档内也校验存在 `vendor/platform-tools-win/adb.exe`。
- Linux `.deb` 已用重构后的同一份源码重建；冒烟测试 7 个页面/接口均返回 200，非法 Host 头返回 403（见 `BUILD.md`）。
- **托盘改造的专项验证**：`go vet`（linux+cgo / windows-amd64）与单元测试全通过；`NOTIFYICONDATAW_V2_SIZE` 952 编译期断言通过，尺寸实测值来自 mingw-w64 `shellapi.h`；构建脚本校验 x64 exe 内确实含 `.ico` 与 PNG 的完整字节。Linux 侧做了真实 X11 回归：`wmctrl -ic` 关窗后窗口隐藏但进程存活、`/api/state` 仍返回 200、非法 Host 仍 403、`GET /api/raise` 与「第二次启动」都能把窗口恢复回来、SIGTERM 才真正停服；任务栏截图见 `docs/screenshot-tray.png`（蓝色 A 图标已出现在通知区域）。
- **Wine 11.0 实机回归（1.1.0，全新前缀）**：双击启动 → 窗口标题 `PC Web ADB (PWA) 1.1.0 · ADB 设备管理器` 正常渲染、消息循环可交互（点击「打开界面」成功唤起浏览器）；内置 adb 自动释放到 `%LOCALAPPDATA%\PC Web ADB\adb\34.0.4\` 并启动；真实设备（小米电视 192.168.2.192:5555）连接、截图（1920×1080 PNG）、文件列表、`/api/recover`（kill-server → start-server → 重启后设备进入 unauthorized 状态再自动恢复授权）全部实测通过；运行日志 `%APPDATA%\PC Web ADB\logs\pcwebadb.log` 正常落盘（含中文错误指引）；关窗后窗口隐藏、服务继续，二次启动直接唤起已有窗口而不新开服务。
- **免安装 exe 的文件图标**：`packaging/windows/versioninfo.rc`（图标 + 简体中文 VERSIONINFO）经 mingw-w64 `windres` 编译为 COFF `.syso` 由 Go 链接器自动链入，构建脚本校验 PE `.rsrc` 节存在且含 UTF-16 产品名 `PC Web ADB (PWA)`；资源管理器/任务栏/Alt-Tab 不再显示 Go 默认图标。

**未验证：** 当前构建环境为 Linux（银河麒麟 V10 SP1），**没有 Windows 7 实机**。上述 Wine 实测覆盖了启动、窗口消息、adb 释放、设备链路与日志，但 NSIS 安装/卸载流程、USB 驱动加载（Wine 无法访问宿主 USB）与 Explorer 通知区域的实际外观仍需在真机上确认。首次部署建议先跑一次 `PCWebADB.exe`，若 adb 不可用，查看日志文件 `%APPDATA%\PC Web ADB\logs\pcwebadb.log`（任何启动失败、panic 都会写在这里）。

---

## 9. 常见问题

- **提示「未找到 adb.exe」/「未能找到可用的 adb 组件」**：当前版本已把 adb 内嵌进 exe，正常情况下不会再出现。若仍出现，说明用户目录不可写或安全软件拦截了释放：改用完整安装包，或把 `adb.exe`、`AdbWinApi.dll`、`AdbWinUsbApi.dll` 三个文件放到 exe 同目录；弹窗里会附具体失败原因，「诊断日志」页也有记录。
- **adb 路径显示正常但所有操作都失败**：先看「诊断日志」是否有 UCRT 警告。Win7 SP1 缺 KB2999226 / VC++ 2015–2019 运行库时 `adb.exe` 根本无法加载，Go 程序本体仍可启动，症状就是「界面正常但 ADB 不工作」。
- **双击后没有窗口 / 一闪而过**：多为缺少 UCRT，导致 `adb.exe` 启动失败。安装 KB2999226 或 VC++ 2015–2019 运行库。
- **页面空白或样式错乱**：使用了 IE。请安装 Chrome / Edge（Win7 支持到 109）/ Firefox，或复制地址到现代浏览器打开。
- **`adb devices` 为空**：安装手机厂商 USB 驱动、确认已开启 USB 调试并授权；无线连接需 Android 11+ 的「无线调试」配对。
- **关掉窗口后程序还在跑 / 怎么彻底退出**：这是预期行为，服务会在后台常驻。右键通知区域的蓝色「A」图标 →「退出 PC Web ADB (PWA)」，或点控制窗口的「退出」按钮。若通知区域隐藏了图标，再次双击 exe 会把窗口唤回来。
- **找不到托盘图标**：Win7 默认会把不活跃图标折叠到通知区域的小三角里，点「自定义」把 PC Web ADB (PWA) 设为「显示图标和通知」。若系统完全没有通知区域，程序会弹窗并自动退回「关窗即停止服务」。
- **每次操作闪黑框**：已通过 `CREATE_NO_WINDOW` 消除；若仍出现，说明调用的是外部 `scrcpy` 控制台程序，属正常。
- **投屏按钮提示找不到 scrcpy**：Windows 需自行下载 scrcpy 并加入 PATH（可选功能，不影响其余模块）。
