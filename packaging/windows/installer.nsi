; PC Web ADB (PWA) - Windows 7 x64 NSIS installer (Unicode / SimpChinese)
; Rebuilt from this source by packaging/windows/build-win.sh, which injects ROOT.
; The script is compiled with -INPUTCHARSET UTF8 from a UTF-8 BOM copy.

Unicode true
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "x64.nsh"

!define APPNAME "PC Web ADB (PWA)"
!define APPVER "1.1.0"
!define REGKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\PCWebADB"

Name "${APPNAME} ${APPVER}"
Caption "${APPNAME} ${APPVER} 安装程序"
BrandingText "PC Web ADB (PWA) · GPL-3.0-or-later · 参考 sky22333/skyadb"
OutFile "${ROOT}/dist/PCWebADB-1.1.0-win7-setup.exe"
InstallDir "$PROGRAMFILES64\PC Web ADB (PWA)"
InstallDirRegKey HKLM "Software\PC Web ADB (PWA)" "InstallDir"
RequestExecutionLevel admin
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "1.1.0.0"
VIAddVersionKey /LANG=1033 "ProductName" "PC Web ADB (PWA)"
VIAddVersionKey /LANG=1033 "FileVersion" "1.1.0.0"
VIAddVersionKey /LANG=1033 "FileDescription" "Desktop ADB device manager (Windows 7 x64)"
VIAddVersionKey /LANG=1033 "LegalCopyright" "GPL-3.0-or-later; adb is Apache-2.0 / Android SDK"
VIAddVersionKey /LANG=1033 "Comments" "Feature set follows github.com/sky22333/skyadb (GPL-3.0)"

!define MUI_ABORTWARNING
!define MUI_ICON "${NSISDIR}/Contrib/Graphics/Icons/modern-install.ico"
!define MUI_UNICON "${NSISDIR}/Contrib/Graphics/Icons/modern-uninstall.ico"
!define MUI_WELCOMEPAGE_TEXT "本程序将安装 ${APPNAME} ${APPVER} —— 一款参考开源项目 SkyADB（GPL-3.0）实现的 PC 端 ADB 设备管理器。$\r$\n$\r$\n本版本仅支持 64 位 Windows 7 SP1 或更高系统。主程序为 x64 单文件，已内嵌适配 Windows 7 的 Android SDK Platform Tools 34.0.4（32 位 adb + 两个 DLL），因此单独拷贝 PCWebADB-x64.exe 也能直接双击运行。$\r$\n$\r$\n支持无线配对、设备发现、应用与文件管理、截图、Logcat、Shell 与虚拟遥控器。关闭窗口不会停止服务，程序会缩到任务栏通知区域的小图标继续后台运行，右键图标可显示窗口或退出。$\r$\n$\r$\n请确认已安装通用 C 运行库（KB2999226 / VC++ 2015-2019，含 32 位运行库），且装有 Chrome / Edge / Firefox 等现代浏览器。"
!define MUI_COMPONENTSPAGE_SMALLDESC
!define MUI_FINISHPAGE_RUN "$INSTDIR\PCWebADB.exe"
!define MUI_FINISHPAGE_RUN_TEXT "立即启动 PC Web ADB (PWA)"
!define MUI_FINISHPAGE_SHOWREADME "$INSTDIR\WINDOWS.md"
!define MUI_FINISHPAGE_SHOWREADME_TEXT "查看 Windows 版使用说明"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION "OpenReadme"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${ROOT}/LICENSE"
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "SimpChinese"

Function OpenReadme
  ExecShell "open" "$INSTDIR\WINDOWS.md"
FunctionEnd

Function .onInit
  SetRegView 64
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP|MB_OK "本版本仅支持 64 位 Windows 7 SP1 或更高系统。$\r$\n如需在 32 位系统上使用，请自行从源码构建。"
    Abort
  ${EndIf}
  ; The bundled platform-tools adb.exe is a 32-bit PE; on Win7 x64 it needs
  ; the 32-bit UCRT from SysWOW64. System32 is accepted for external 64-bit
  ; adb binaries.
  IfFileExists "$WINDIR\SysWOW64\ucrtbase.dll" ucrt_ok 0
    IfFileExists "$SYSDIR\ucrtbase.dll" ucrt_ok 0
      MessageBox MB_ICONEXCLAMATION|MB_OK "未检测到通用 C 运行库 (UCRT / ucrtbase.dll)。$\r$\n$\r$\nWindows 7 SP1 需要先安装 32 位与 64 位 VC++ 2015-2019 运行库（或 KB2999226），否则内置的 adb.exe 无法启动。$\r$\n$\r$\n安装仍可继续，但请先补齐该运行库。"
  ucrt_ok:
FunctionEnd

Section "PC Web ADB (PWA) 主程序（64 位，仅 Win7 x64）" SEC_MAIN
  SectionIn RO
  SetRegView 64
  SetOutPath "$INSTDIR"
  File "/oname=PCWebADB.exe" "${ROOT}/dist/PCWebADB-x64.exe"
  ; adb.exe 34.0.4 + AdbWinApi.dll + AdbWinUsbApi.dll are go:embed'ed into
  ; PCWebADB.exe and extracted to %LOCALAPPDATA% on first use. Installing them
  ; again as sibling files would duplicate ~6 MiB of identical bytes.
  File "/oname=platform-tools.properties" "${ROOT}/vendor/platform-tools-win/source.properties"
  File "/oname=LICENSE.txt" "${ROOT}/LICENSE"
  File "/oname=ADB-NOTICE.txt" "${ROOT}/vendor/platform-tools-win/NOTICE.txt"
  File "/oname=README.md" "${ROOT}/README.md"
  File "/oname=WINDOWS.md" "${ROOT}/docs/WINDOWS.md"
  File "/oname=UPSTREAM-REFERENCE.md" "${ROOT}/packaging/UPSTREAM-REFERENCE.md"
  ; GPL corresponding source is shipped as dist/pcwebadb-1.1.0-source.tar.gz
  ; and inside the .deb. Including it here would push this makensis build over
  ; its ~16 MiB datablock limit and crash with SIGBUS.

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "Software\PC Web ADB (PWA)" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${REGKEY}" "DisplayName" "PC Web ADB (PWA) 1.1.0"
  WriteRegStr HKLM "${REGKEY}" "DisplayVersion" "1.1.0"
  WriteRegStr HKLM "${REGKEY}" "Publisher" "WorkBuddy"
  WriteRegStr HKLM "${REGKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${REGKEY}" "DisplayIcon" "$INSTDIR\PCWebADB.exe,0"
  WriteRegStr HKLM "${REGKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "${REGKEY}" "URLInfoAbout" "https://github.com/sky22333/skyadb"
  WriteRegStr HKLM "${REGKEY}" "HelpLink" "https://www.workbuddy.cn"
  WriteRegDWORD HKLM "${REGKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${REGKEY}" "NoRepair" 1

  CreateDirectory "$SMPROGRAMS\PC Web ADB (PWA)"
  CreateShortCut "$SMPROGRAMS\PC Web ADB (PWA)\PC Web ADB (PWA).lnk" "$INSTDIR\PCWebADB.exe"
  CreateShortCut "$SMPROGRAMS\PC Web ADB (PWA)\使用说明.lnk" "$INSTDIR\WINDOWS.md"
  CreateShortCut "$SMPROGRAMS\PC Web ADB (PWA)\卸载 PC Web ADB (PWA).lnk" "$INSTDIR\Uninstall.exe"
SectionEnd

Section "创建桌面快捷方式" SEC_DESKTOP
  CreateShortCut "$DESKTOP\PC Web ADB (PWA).lnk" "$INSTDIR\PCWebADB.exe"
SectionEnd

LangString DESC_SEC_MAIN ${LANG_SIMPCHINESE} "必需：64 位主程序（已内嵌 adb 34.0.4，单文件即可运行）、许可与源码归档。仅适用于 Win7 x64 及以上系统。"
LangString DESC_SEC_DESKTOP ${LANG_SIMPCHINESE} "在桌面创建指向主程序的快捷方式。"
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SEC_MAIN} $(DESC_SEC_MAIN)
  !insertmacro MUI_DESCRIPTION_TEXT ${SEC_DESKTOP} $(DESC_SEC_DESKTOP)
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Section "Uninstall"
  SetRegView 64
  Delete "$INSTDIR\PCWebADB.exe"
  Delete "$INSTDIR\adb.exe"
  Delete "$INSTDIR\AdbWinApi.dll"
  Delete "$INSTDIR\AdbWinUsbApi.dll"
  Delete "$INSTDIR\platform-tools.properties"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\ADB-NOTICE.txt"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\WINDOWS.md"
  Delete "$INSTDIR\UPSTREAM-REFERENCE.md"
  Delete "$INSTDIR\Uninstall.exe"
  Delete "$DESKTOP\PC Web ADB (PWA).lnk"
  Delete "$SMPROGRAMS\PC Web ADB (PWA)\PC Web ADB (PWA).lnk"
  Delete "$SMPROGRAMS\PC Web ADB (PWA)\使用说明.lnk"
  Delete "$SMPROGRAMS\PC Web ADB (PWA)\卸载 PC Web ADB (PWA).lnk"
  RMDir "$SMPROGRAMS\PC Web ADB (PWA)"
  DeleteRegKey HKLM "${REGKEY}"
  DeleteRegKey HKLM "Software\PC Web ADB (PWA)"
  RMDir "$INSTDIR"
  ; Per-user settings in %APPDATA%\PC Web ADB are intentionally preserved.
SectionEnd
