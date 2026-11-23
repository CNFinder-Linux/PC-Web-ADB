package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Adb wraps the official Android SDK Platform Tools `adb` binary.
//
// SkyADB (the Android reference app) implements the ADB protocol in-process via
// Kadb. On the desktop the well-supported equivalent is to drive the official
// `adb` client (same ADB protocol, plus TLS wireless pairing), which is shipped
// inside the .deb so the tool works out of the box.
type Adb struct {
	Bin      string
	Settings func() Settings
	Diag     *Diagnostics
}

func (a *Adb) diag(level, op, msg string) {
	if a.Diag != nil {
		a.Diag.Add(level, op, msg)
	}
}

func (a *Adb) cmdTimeout() time.Duration {
	sec := a.Settings().CommandTimeout
	if sec < 1 || sec > 600 {
		sec = 20 // defensive clamp; Settings.Set/normalizeSettings also enforce this
	}
	return time.Duration(sec) * time.Second
}

type cmdResult struct {
	Stdout []byte
	Stderr []byte
	Err    error
}

func (a *Adb) run(ctx context.Context, timeout time.Duration, args ...string) cmdResult {
	if timeout <= 0 {
		timeout = a.cmdTimeout()
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, a.Bin, args...)
	tuneCommand(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	err := cmd.Run()
	return cmdResult{Stdout: out.Bytes(), Stderr: errb.Bytes(), Err: err}
}

// adbErrorHints maps the most common adb failure strings to actionable
// Chinese guidance. The original English text is kept inside the message so
// callers that match on it (e.g. the connect handler) keep working, and the
// diagnostics page still shows what adb actually printed.
var adbErrorHints = []struct{ match, hint string }{
	{"unauthorized", "设备未授权：请查看设备屏幕上的「USB/网络调试授权」弹窗，勾选「始终允许」并点确定。" +
		"若没有弹窗：到设备的开发者选项里先「撤销 USB 调试授权」，然后点本程序里的「重启 ADB 服务」再重新连接。"},
	{"device offline", "设备离线：常见于 USB 休眠、WiFi 中断或设备重启。重新插拔数据线 / 检查网络，然后点「重启 ADB 服务」再重试。"},
	{"no permissions", "无 USB 访问权限：Windows 请安装该设备的 USB 驱动（Google USB Driver，或小米/华为等官方助手的自动装驱动功能）；" +
		"Linux 需要配置 udev 规则或将用户加入 plugdev 组。"},
	{"more than one device", "检测到多台设备：请先在顶部设备选择器中选中具体设备再操作。"},
	{"device '", "设备不在线：可能已断开连接，请刷新设备列表后重试。"},
	{"doesn't match this client", "adb 服务版本不匹配：本机还驻留着其它 adb 工具启动的服务。点本程序的「重启 ADB 服务」即可切换到内置 adb。"},
	{"cannot connect to daemon", "adb 服务无法连接：可能被安全软件拦截或端口被占用，点「重启 ADB 服务」重试。"},
	{"failed to start daemon", "adb 服务无法启动：请检查安全软件是否拦截了 adb.exe。"},
	{"protocol fault", "adb 连接异常：点「重启 ADB 服务」后重试。"},
	{"failed to connect", "连接失败：目标端口未开放。请确认设备与电脑在同一网络、设备已开启网络/无线调试；" +
		"Android 11+ 需用「无线配对」拿到配对码先配对，再连接连接端口（两个端口号不同）。"},
	{"connection refused", "连接被拒绝：设备端口未开放或 IP 不正确。"},
	{"closed", "连接被设备中断：请重试；若反复出现，点「重启 ADB 服务」。"},
	{"told to exit", "adb 服务被终止（常见于同时运行了其它 adb 管理工具）。点「重启 ADB 服务」可恢复。"},
}

// friendlyAdbError wraps an adb error message with concrete guidance.
func friendlyAdbError(msg string) string {
	low := strings.ToLower(msg)
	for _, m := range adbErrorHints {
		if strings.Contains(low, m.match) {
			return m.hint + "\n（adb 原始输出：" + strings.TrimSpace(msg) + "）"
		}
	}
	return msg
}

// Text runs adb and returns combined stdout/stderr as text.
func (a *Adb) Text(args ...string) (string, error) {
	r := a.run(context.Background(), 0, args...)
	s := strings.TrimSpace(string(r.Stdout))
	e := strings.TrimSpace(string(r.Stderr))
	if r.Err != nil {
		msg := strings.TrimSpace(s + "\n" + e)
		a.diag("error", "adb "+strings.Join(args, " "), msg)
		return s, fmt.Errorf("%s", friendlyAdbError(msg))
	}
	return s, nil
}

// Raw runs adb and returns raw stdout (used for PNG screenshots).
func (a *Adb) Raw(args ...string) ([]byte, error) {
	r := a.run(context.Background(), 0, args...)
	if r.Err != nil {
		msg := strings.TrimSpace(string(r.Stderr) + " " + string(r.Stdout))
		a.diag("error", "adb "+strings.Join(args, " "), msg)
		return nil, fmt.Errorf("%s", friendlyAdbError(msg))
	}
	return r.Stdout, nil
}

// Device prefixes every command with -s <serial>.
func withSerial(serial string, args ...string) []string {
	if serial == "" {
		return args
	}
	return append([]string{"-s", serial}, args...)
}

func (a *Adb) DeviceText(serial string, args ...string) (string, error) {
	return a.Text(withSerial(serial, args...)...)
}

// Shell executes one remote shell command. `remote` is passed as a single
// argument so that adb does not re-join it with local shell semantics.
func (a *Adb) Shell(serial, remote string) (string, error) {
	if err := validateRemoteCommand(remote); err != nil {
		return "", err
	}
	return a.DeviceText(serial, "shell", remote)
}

// shq quotes a string for /system/bin/sh using single quotes.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var badControl = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

// validateRemoteCommand mirrors SkyADB's CommandInputParser: block control
// characters that could smuggle an extra command into the local process.
func validateRemoteCommand(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("命令不能为空")
	}
	if badControl.MatchString(s) {
		return errors.New("命令包含非法控制字符")
	}
	return nil
}

// validateRemotePath mirrors SkyADB's DevicePathValidator.
// systemRoots are mount points where creating, renaming or deleting entries
// would damage the device. Listing and pulling stay allowed - browsing up from
// /sdcard to / is a normal thing to do in a file manager.
var systemRoots = []string{"/", "/data", "/system", "/proc", "/dev"}

func validateRemotePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("路径不能为空")
	}
	if badControl.MatchString(p) || strings.ContainsAny(p, "\r\n") {
		return errors.New("路径包含非法字符")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return errors.New("路径不允许包含 ..")
		}
	}
	return nil
}

// validateMutablePath additionally rejects the system mount points, for
// operations that change the filesystem.
func validateMutablePath(p string) error {
	if err := validateRemotePath(p); err != nil {
		return err
	}
	norm := normalizeRemotePath(p)
	for _, root := range systemRoots {
		if norm == root || (root != "/" && strings.HasPrefix(norm, root+"/")) {
			return errors.New("不允许对系统目录及其子目录执行该操作，请进入 /sdcard 下的具体目录")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Devices
// ---------------------------------------------------------------------------

type Device struct {
	Serial      string `json:"serial"`
	State       string `json:"state"`
	Model       string `json:"model"`
	Product     string `json:"product"`
	DeviceName  string `json:"deviceName"`
	TransportID string `json:"transportId"`
	Transport   string `json:"transport"`
}

var propRE = regexp.MustCompile(`^(\S+):(\S+)$`)

func (a *Adb) Devices() ([]Device, error) {
	out, err := a.Text("devices", "-l")
	if err != nil {
		return nil, err
	}
	list := []Device{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "* daemon") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		d := Device{Serial: f[0], State: f[1], Transport: "network"}
		if !strings.Contains(f[0], ":") && !strings.HasPrefix(f[0], "emulator-") {
			d.Transport = "usb"
		}
		for _, kv := range f[2:] {
			if m := propRE.FindStringSubmatch(kv); len(m) == 3 {
				switch m[1] {
				case "model":
					d.Model = m[2]
				case "product":
					d.Product = m[2]
				case "device":
					d.DeviceName = m[2]
				case "transport_id":
					d.TransportID = m[2]
				}
			}
		}
		list = append(list, d)
	}
	return list, nil
}

// ---------------------------------------------------------------------------
// Device information
// ---------------------------------------------------------------------------

var getpropRE = regexp.MustCompile(`^\[([^\]]+)\]:\s*\[(.*)\]\s*$`)

func (a *Adb) DeviceInfo(serial string) (map[string]string, error) {
	info := map[string]string{}
	props, err := a.Shell(serial, "getprop")
	if err != nil {
		return nil, err
	}
	all := map[string]string{}
	for _, line := range strings.Split(props, "\n") {
		if m := getpropRE.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 3 {
			all[m[1]] = m[2]
		}
	}
	keys := []struct{ key, label string }{
		{"ro.product.brand", "品牌"},
		{"ro.product.manufacturer", "制造商"},
		{"ro.product.model", "型号"},
		{"ro.product.device", "设备代号"},
		{"ro.build.version.release", "Android 版本"},
		{"ro.build.version.sdk", "SDK 版本"},
		{"ro.build.display.id", "系统版本号"},
		{"ro.build.version.security_patch", "安全补丁"},
		{"ro.product.cpu.abi", "CPU 架构"},
		{"ro.serialno", "序列号"},
		{"ro.build.version.incremental", "增量版本"},
		{"ro.board.platform", "平台"},
	}
	for _, k := range keys {
		if v := all[k.key]; v != "" {
			info[k.label] = v
		}
	}
	// Extra probes; failures are non-fatal, matching the reference app's
	// tolerant "device info" screen.
	extra := []struct{ label, cmd string }{
		{"分辨率", "wm size"},
		{"屏幕密度", "wm density"},
		{"电池", "dumpsys battery | grep -E 'level|status|temperature' | tr '\\n' ' '"},
		{"存储空间", "df -h /data | tail -n 1"},
		{"IP 地址", "ip -o -4 addr show | awk '{print $2, $4}' | tr '\\n' ' '"},
		{"运行时长", "uptime"},
		{"SELinux", "getenforce"},
	}
	for _, e := range extra {
		if out, err := a.Shell(serial, e.cmd); err == nil && strings.TrimSpace(out) != "" {
			info[e.label] = strings.TrimSpace(out)
		}
	}
	info["设备序列号"] = serial
	return info, nil
}

// ---------------------------------------------------------------------------
// Applications
// ---------------------------------------------------------------------------

type AppInfo struct {
	Package string `json:"package"`
	ApkPath string `json:"apkPath"`
	Kind    string `json:"kind"` // user | system
	Enabled bool   `json:"enabled"`
}

var pkgLineRE = regexp.MustCompile(`^package:(.*)=([^=\s]+)$`)

func (a *Adb) Apps(serial, kind string) ([]AppInfo, error) {
	var args []string
	switch kind {
	case "user":
		args = []string{"-3", "-f"}
	case "system":
		args = []string{"-s", "-f"}
	default:
		args = []string{"-f"}
	}
	out, err := a.DeviceText(serial, append([]string{"shell", "pm", "list", "packages"}, args...)...)
	if err != nil {
		return nil, err
	}
	disabled := map[string]bool{}
	if d, err := a.DeviceText(serial, "shell", "pm", "list", "packages", "-d"); err == nil {
		for _, l := range strings.Split(d, "\n") {
			disabled[strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "package:"))] = true
		}
	}
	apps := []AppInfo{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		app := AppInfo{Kind: kind, Enabled: true}
		if m := pkgLineRE.FindStringSubmatch(line); len(m) == 3 {
			app.ApkPath = m[1]
			app.Package = m[2]
		} else {
			app.Package = strings.TrimPrefix(line, "package:")
		}
		if app.Package == "" {
			continue
		}
		if disabled[app.Package] {
			app.Enabled = false
		}
		if strings.Contains(app.ApkPath, "/system/") || strings.Contains(app.ApkPath, "/vendor/") || strings.Contains(app.ApkPath, "/product/") {
			app.Kind = "system"
		} else if app.ApkPath != "" {
			app.Kind = "user"
		}
		apps = append(apps, app)
	}
	return apps, nil
}

func (a *Adb) AppAction(serial, action, pkg string) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`).MatchString(pkg) {
		return "", errors.New("非法包名")
	}
	var cmd string
	switch action {
	case "launch":
		cmd = "monkey -p " + shq(pkg) + " -c android.intent.category.LAUNCHER 1"
	case "stop":
		cmd = "am force-stop " + shq(pkg)
	case "uninstall":
		cmd = "pm uninstall " + shq(pkg)
	case "clear":
		cmd = "pm clear " + shq(pkg)
	case "enable":
		cmd = "pm enable " + shq(pkg)
	case "disable":
		cmd = "pm disable-user --user 0 " + shq(pkg)
	case "export":
		out, err := a.DeviceText(serial, "shell", "pm", "path", shq(pkg))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil
	default:
		return "", errors.New("未知操作: " + action)
	}
	return a.Shell(serial, cmd)
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

type RemoteFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	Link    bool   `json:"link"`
	Size    int64  `json:"size"`
	Perms   string `json:"perms"`
	Owner   string `json:"owner"`
	ModTime string `json:"modTime"`
}

var lsLineRE = regexp.MustCompile(`^([dlcbps-])([rwxstST\-]{9})[.+]?\s+\d+\s+(\S+)\s+(\S+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(.+)$`)

// normalizeRemotePath collapses duplicate separators ("/sdcard//DCIM") and
// drops a trailing slash so that joinRemote stays predictable.
func normalizeRemotePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func (a *Adb) ListFiles(serial, dir string) ([]RemoteFile, error) {
	if err := validateRemotePath(dir); err != nil {
		return nil, err
	}
	dir = normalizeRemotePath(dir)
	// Toybox `ls -l <symlink>` prints the link itself, so /sdcard (a symlink to
	// /storage/self/primary) used to come back as a single bogus row named
	// "/sdcard". A trailing slash forces the dereference; the plain form stays
	// as a fallback for regular files and for ls variants that dislike it.
	// `adb shell` is not a TTY, so no ANSI colour is emitted; avoid the
	// --color flag because older toybox builds do not implement it.
	//
	// ls exits non-zero when *any* entry is unreadable ("//init: Permission
	// denied") even though the rest lists fine, so the exit code alone must not
	// be treated as failure - only a result with zero parseable rows is.
	parse := func(out string) []RemoteFile {
		files := []RemoteFile{}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimRight(line, "\r")
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "total ") {
				continue
			}
			m := lsLineRE.FindStringSubmatch(line)
			if len(m) != 9 {
				continue
			}
			name := m[8]
			if strings.Contains(name, " -> ") {
				name = strings.SplitN(name, " -> ", 2)[0]
			}
			name = strings.TrimSuffix(name, "/")
			// ls echoes the argument path when it lists the operand itself.
			if strings.HasPrefix(name, "/") {
				if normalizeRemotePath(name) == dir {
					continue
				}
				name = path.Base(name)
			}
			size, _ := strconv.ParseInt(m[5], 10, 64)
			files = append(files, RemoteFile{
				Name: name, Path: joinRemote(dir, name),
				Dir: m[1] == "d", Link: m[1] == "l", Size: size,
				Perms: m[1] + m[2], Owner: m[3] + ":" + m[4],
				ModTime: m[6] + " " + m[7],
			})
		}
		return files
	}

	first := a.run(context.Background(), 0, withSerial(serial, "shell", "ls -lA "+shq(dir+"/"))...)
	files := parse(string(first.Stdout))
	if len(files) > 0 {
		return files, nil
	}
	second := a.run(context.Background(), 0, withSerial(serial, "shell", "ls -lA "+shq(dir))...)
	if files = parse(string(second.Stdout)); len(files) > 0 {
		return files, nil
	}
	msg := firstLine(string(second.Stderr))
	if msg == "" {
		msg = firstLine(string(first.Stderr))
	}
	if msg == "" {
		msg = "空目录或无法读取：" + dir
	}
	return nil, errors.New("无法读取 " + dir + "：" + msg)
}

// firstLine returns the first non-empty line, for concise error messages.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(strings.TrimRight(line, "\r")); t != "" {
			return t
		}
	}
	return ""
}

func joinRemote(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

// IsDir reports whether path is a directory on the device. Directories cannot
// be pulled, so the download endpoint checks this first instead of handing back
// an empty file with HTTP 200.
func (a *Adb) IsDir(serial, p string) bool {
	if err := validateRemotePath(p); err != nil {
		return false
	}
	out, err := a.Shell(serial, "[ -d "+shq(p)+" ] && echo yes || echo no")
	if err != nil {
		return false
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]) == "yes"
}

func (a *Adb) FileAction(serial, action, path, name string) (string, error) {
	if err := validateMutablePath(path); err != nil {
		return "", err
	}
	path = normalizeRemotePath(path)
	var cmd string
	switch action {
	case "mkdir":
		if err := validateName(name); err != nil {
			return "", err
		}
		cmd = "mkdir -p " + shq(joinRemote(path, name))
	case "delete":
		cmd = "rm -r -f " + shq(path)
	case "rename":
		if err := validateName(name); err != nil {
			return "", err
		}
		parent := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			parent = path[:i]
			if parent == "" {
				parent = "/"
			}
		}
		cmd = "mv " + shq(path) + " " + shq(joinRemote(parent, name))
	case "touch":
		if err := validateName(name); err != nil {
			return "", err
		}
		cmd = "touch " + shq(joinRemote(path, name))
	default:
		return "", errors.New("未知操作: " + action)
	}
	return a.Shell(serial, cmd)
}

func validateName(n string) error {
	if n == "" || strings.ContainsAny(n, "/\\\r\n") || n == "." || n == ".." {
		return errors.New("非法名称")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Screenshot
// ---------------------------------------------------------------------------

// pngSignature is the 8-byte PNG file header. Some vendor ROMs (MiTV/MIUI TV
// boxes, several MStar/MTK builds) print a banner on stdout from their
// `screencap` wrapper - "Init wrapper sys mutex successful. Pid:1" - so the
// marker has to be searched for instead of being assumed at offset 0. Treating
// that banner as "not a PNG" used to push us onto the `adb shell` path, whose
// pty rewrites LF as CRLF and silently corrupts every frame.
var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

var pngIEND = []byte{0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}

// validPNG reports whether buf is a complete, decodable PNG. Checking only the
// signature is not enough: CRLF-mangled streams keep the first bytes but no
// longer decode.
func validPNG(buf []byte) bool {
	if len(buf) < len(pngSignature) {
		return false
	}
	if !bytes.HasPrefix(buf, pngSignature) || !bytes.HasSuffix(buf, pngIEND) {
		return false
	}
	_, err := png.DecodeConfig(bytes.NewReader(buf))
	return err == nil
}

// pngFrom trims any leading console noise and returns the PNG, or nil.
func pngFrom(buf []byte) []byte {
	if i := bytes.Index(buf, pngSignature); i > 0 {
		buf = buf[i:]
	}
	if validPNG(buf) {
		return buf
	}
	return nil
}

// Screenshot captures the framebuffer as a valid PNG.
//
// Order matters: `exec-out` keeps the stream binary-safe and only needs the
// banner skipped, so it is tried first. Capturing to a file on the device and
// pulling it is the fallback for builds where the stream is still mangled, and
// a plain `adb shell` with CRLF repair is the last resort for very old devices.
// Every candidate is validated before being returned, so a broken frame is
// reported as an error instead of being served as `image/png`.
func (a *Adb) Screenshot(serial string) ([]byte, error) {
	var firstErr error
	if out, err := a.Raw(withSerial(serial, "exec-out", "screencap", "-p")...); err == nil {
		if png := pngFrom(out); png != nil {
			return png, nil
		}
	} else {
		firstErr = err
	}

	if png, err := a.screenshotViaFile(serial); err == nil {
		return png, nil
	} else if firstErr == nil {
		firstErr = err
	}

	if out, err := a.Raw(withSerial(serial, "shell", "screencap", "-p")...); err == nil {
		if png := pngFrom(out); png != nil {
			return png, nil
		}
		if png := pngFrom(bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))); png != nil {
			return png, nil
		}
	} else if firstErr == nil {
		firstErr = err
	}

	if firstErr == nil {
		firstErr = errors.New("截图失败：设备返回的数据不是有效的 PNG")
	} else {
		firstErr = errors.New("截图失败：" + firstErr.Error())
	}
	a.diag("error", "screenshot", firstErr.Error())
	return nil, firstErr
}

// screenshotViaFile writes the PNG on the device and pulls it back, which is
// immune to both stdout banners and pty line-ending translation.
func (a *Adb) screenshotViaFile(serial string) ([]byte, error) {
	remote := fmt.Sprintf("/data/local/tmp/skyadb-shot-%d-%d.png", os.Getpid(), time.Now().UnixNano())
	if _, err := a.DeviceText(serial, "shell", "screencap", "-p", remote); err != nil {
		return nil, err
	}
	defer func() { _, _ = a.DeviceText(serial, "shell", "rm -f", remote) }()

	dir := tempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	local, err := os.CreateTemp(dir, "shot-*.png")
	if err != nil {
		return nil, err
	}
	name := local.Name()
	local.Close()
	defer os.Remove(name)

	if _, err := a.DeviceText(serial, "pull", remote, name); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if png := pngFrom(data); png != nil {
		return png, nil
	}
	return nil, errors.New("拉取到的截图不是有效的 PNG")
}

// ---------------------------------------------------------------------------
// Input / remote control
// ---------------------------------------------------------------------------

var keyRE = regexp.MustCompile(`^[A-Z0-9_]+$`)

func (a *Adb) InputKey(serial, key string) (string, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	if !strings.HasPrefix(key, "KEYCODE_") {
		if _, err := strconv.Atoi(key); err != nil {
			key = "KEYCODE_" + key
		}
	}
	if !keyRE.MatchString(key) {
		return "", errors.New("非法按键")
	}
	return a.DeviceText(serial, "shell", "input", "keyevent", key)
}

func (a *Adb) InputText(serial, text string) (string, error) {
	text = strings.ReplaceAll(text, "'", "")
	if err := validateRemoteCommand(text); err != nil {
		return "", err
	}
	return a.DeviceText(serial, "shell", "input", "text", shq(strings.ReplaceAll(text, " ", "%s")))
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func (a *Adb) Version() string {
	s, err := a.Text("version")
	if err != nil {
		return "unknown"
	}
	if lines := strings.Split(s, "\n"); len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return s
}

func (a *Adb) Connect(host string, port int) (string, error) {
	return a.Text("connect", fmt.Sprintf("%s:%d", host, port))
}

func (a *Adb) Pair(host string, port int, code string) (string, error) {
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		return "", errors.New("配对码应为 6 位数字")
	}
	return a.Text("pair", fmt.Sprintf("%s:%d", host, port), code)
}

func (a *Adb) Disconnect(serial string) (string, error) {
	return a.Text("disconnect", serial)
}
