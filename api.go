package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	adb      *Adb
	store    *Store
	diag     *Diagnostics
	mux      *http.ServeMux
	mirrorMu sync.Mutex
	mirrors  map[string]*exec.Cmd
}

func NewServer(a *Adb, s *Store, d *Diagnostics, web http.FileSystem) *Server {
	srv := &Server{adb: a, store: s, diag: d, mirrors: map[string]*exec.Cmd{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/state", srv.state)
	mux.HandleFunc("/api/devices", srv.devices)
	mux.HandleFunc("/api/connect", srv.connect)
	mux.HandleFunc("/api/pair", srv.pair)
	mux.HandleFunc("/api/disconnect", srv.disconnect)
	mux.HandleFunc("/api/discover", srv.discover)
	mux.HandleFunc("/api/recent", srv.recent)
	mux.HandleFunc("/api/info", srv.info)
	mux.HandleFunc("/api/apps", srv.apps)
	mux.HandleFunc("/api/apps/action", srv.appAction)
	mux.HandleFunc("/api/apps/install", srv.appInstall)
	mux.HandleFunc("/api/files", srv.files)
	mux.HandleFunc("/api/files/upload", srv.fileUpload)
	mux.HandleFunc("/api/files/download", srv.fileDownload)
	mux.HandleFunc("/api/files/action", srv.fileAction)
	mux.HandleFunc("/api/screenshot", srv.screenshot)
	mux.HandleFunc("/api/logcat", srv.logcat)
	mux.HandleFunc("/api/logcat/stream", srv.logcatStream)
	mux.HandleFunc("/api/shell", srv.shell)
	mux.HandleFunc("/api/key", srv.key)
	mux.HandleFunc("/api/mirror", srv.mirror)
	mux.HandleFunc("/api/diagnostics", srv.diagnostics)
	mux.HandleFunc("/api/settings", srv.settings)
	mux.HandleFunc("/api/recover", srv.adbRecover)
	mux.HandleFunc("/api/raise", srv.raise)

	mux.Handle("/", http.FileServer(web))
	srv.mux = mux
	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Loopback-only service: reject DNS rebinding / foreign Host headers.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" && host != "[::1]" && host != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// State-changing endpoints can delete files, uninstall apps and run shell
	// commands. Reject cross-origin browser requests even if a rebinding page
	// managed to make the Host header look local. Non-browser clients that omit
	// Origin (curl, our own GUI) remain supported.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" {
			u, err := url.Parse(o)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
				(u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.mux.ServeHTTP(w, r)
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

type apiResponse struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

func reply(w http.ResponseWriter, data interface{}, err error) {
	replyStatus(w, http.StatusBadRequest, data, err)
}

func replyStatus(w http.ResponseWriter, status int, data interface{}, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(apiResponse{OK: false, Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(apiResponse{OK: true, Data: data})
}

func decode(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func method(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	replyStatus(w, http.StatusMethodNotAllowed, nil, fmt.Errorf("不支持的请求方法 %s", r.Method))
	return false
}

func serialOf(r *http.Request) string { return strings.TrimSpace(r.URL.Query().Get("serial")) }

func (s *Server) log(level, op, msg string) { s.diag.Add(level, op, msg) }

// ---------------------------------------------------------------------------
// State / connection
// ---------------------------------------------------------------------------

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	reply(w, map[string]interface{}{
		"app":        appName,
		"version":    version,
		"platform":   runtime.GOOS,
		"adbPath":    s.adb.Bin,
		"adbVersion": s.adb.Version(),
		"logPath":    logPath(),
		"settings":   s.store.Get(),
		"configDir":  configDir(),
		"reference":  "github.com/sky22333/skyadb @ " + upstreamCommit,
	}, nil)
}

// adbRecover restarts the adb server. It is the fix for the three failure
// modes users hit most often: a stale server left behind by another adb tool
// ("server version doesn't match"), a wedged server ("protocol fault"), and an
// authorization prompt that only appears after kill-server + reconnect.
func (s *Server) adbRecover(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	steps := []string{}
	ok := true

	out, err := s.adb.Text("kill-server")
	if err != nil {
		// "adb server not running" is the normal outcome when nothing is up.
		steps = append(steps, "kill-server："+firstLine(out+" "+err.Error()))
	} else {
		steps = append(steps, "已停止旧的 adb 服务")
	}
	time.Sleep(600 * time.Millisecond)
	if out, err := s.adb.Text("start-server"); err != nil {
		ok = false
		steps = append(steps, "start-server 失败："+firstLine(out+" "+err.Error()))
		s.log("error", "recover", out+" "+err.Error())
	} else {
		steps = append(steps, "已用内置 adb "+s.adb.Version()+" 启动服务")
	}

	// Re-attach the wireless devices we already know about so the user does
	// not have to retype addresses after a restart.
	reconnected := []string{}
	for _, d := range s.store.Recent() {
		addr := net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
		if msg, err := s.adb.Connect(d.Host, d.Port); err == nil &&
			!strings.Contains(msg, "failed") && !strings.Contains(msg, "cannot") &&
			!strings.Contains(msg, "unable") {
			reconnected = append(reconnected, addr)
		}
	}
	if len(reconnected) > 0 {
		steps = append(steps, "已重新连接："+strings.Join(reconnected, "、"))
	}

	msg := "adb 服务已重启"
	if !ok {
		msg = "adb 服务重启失败"
	}
	s.log("info", "recover", strings.Join(steps, "；"))
	reply(w, map[string]interface{}{
		"message": msg, "steps": steps, "reconnected": reconnected,
	}, nil)
}

// raise re-shows the hidden desktop window. A second GUI launch calls this
// endpoint on the already-running instance; it is also handy for the Web UI.
// The platform handler only posts a message / sets a flag, so no GUI API is
// ever touched from this HTTP goroutine.
func (s *Server) raise(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	requestAppRaise()
	reply(w, map[string]bool{"raised": true}, nil)
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	list, err := s.adb.Devices()
	reply(w, list, err)
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	if req.Port == 0 {
		req.Port = s.store.Get().DefaultPort
	}
	if net.ParseIP(req.Host) == nil && !regexp.MustCompile(`^[A-Za-z0-9.\-]+$`).MatchString(req.Host) {
		reply(w, nil, errors.New("非法主机地址"))
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		reply(w, nil, errors.New("端口范围应为 1-65535"))
		return
	}
	out, err := s.adb.Connect(req.Host, req.Port)
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "connect", fmt.Sprintf("%s:%d %s", req.Host, req.Port, out))
	if err != nil {
		reply(w, nil, err)
		return
	}
	if strings.Contains(out, "unable to connect") || strings.Contains(out, "failed to connect") {
		reply(w, nil, errors.New(out))
		return
	}
	serial := fmt.Sprintf("%s:%d", req.Host, req.Port)
	model := ""
	if info, e := s.adb.DeviceInfo(serial); e == nil {
		model = info["型号"]
	}
	s.store.AddRecent(RecentDevice{Host: req.Host, Port: req.Port, Serial: serial, Model: model})
	reply(w, map[string]interface{}{"serial": serial, "message": out}, nil)
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		Code string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	if net.ParseIP(req.Host) == nil && !regexp.MustCompile(`^[A-Za-z0-9.\-]+$`).MatchString(req.Host) {
		reply(w, nil, errors.New("非法主机地址"))
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		reply(w, nil, errors.New("端口范围应为 1-65535"))
		return
	}
	out, err := s.adb.Pair(req.Host, req.Port, req.Code)
	msg := out
	if err == nil {
		// Pairing alone leaves no usable transport: connect to the advertised
		// data port so the device shows up in the list straight away.
		if serial := s.adb.AutoConnectAfterPair(req.Host); serial != "" {
			msg = strings.TrimSpace(out + "，已自动连接 " + serial)
			// Android 11+ uses a pairing port and a different data port. Save
			// the port we actually connected on, otherwise the recent-device
			// entry and /api/recover would retry the useless pairing port.
			connectPort := req.Port
			if _, ps, e := net.SplitHostPort(serial); e == nil {
				if cp, e := strconv.Atoi(ps); e == nil && cp >= 1 && cp <= 65535 {
					connectPort = cp
				}
			}
			_ = s.store.AddRecent(RecentDevice{Host: req.Host, Port: connectPort, Serial: serial})
		} else {
			msg = strings.TrimSpace(out + "，未找到连接端口，请稍候重试或手动填写设备地址")
		}
	}
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "pair", fmt.Sprintf("%s:%d %s", req.Host, req.Port, msg))
	reply(w, map[string]interface{}{"message": msg}, err)
}

func (s *Server) disconnect(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Serial string `json:"serial"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.adb.Disconnect(req.Serial)
	s.log("info", "disconnect", req.Serial+" "+out)
	reply(w, map[string]interface{}{"message": out}, err)
}

func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Range string `json:"range"`
		Port  int    `json:"port"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	st := s.store.Get()
	if req.Port == 0 {
		req.Port = st.DefaultPort
	}
	if req.Port < 1 || req.Port > 65535 {
		reply(w, nil, errors.New("端口范围应为 1-65535"))
		return
	}
	if req.Range == "" {
		req.Range = st.ScanRanges
	}
	if strings.TrimSpace(req.Range) == "" {
		// Also probe the subnets of already-known devices: a phone on a routed
		// 192.168.2.x network is invisible to an interface-based scan.
		req.Range = recentSubnets(s.store.Recent())
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	found, err := s.adb.Discover(ctx, req.Range, req.Port)
	reply(w, found, err)
}

func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		reply(w, s.store.Recent(), nil)
	case http.MethodPost:
		var d RecentDevice
		if err := decode(r, &d); err != nil {
			reply(w, nil, err)
			return
		}
		if d.Port == 0 {
			d.Port = s.store.Get().DefaultPort
		}
		if d.Serial == "" {
			d.Serial = fmt.Sprintf("%s:%d", d.Host, d.Port)
		}
		reply(w, s.store.AddRecent(d), nil)
	case http.MethodDelete:
		s.store.ClearRecent()
		reply(w, []RecentDevice{}, nil)
	default:
		method(w, r, http.MethodGet, http.MethodPost, http.MethodDelete)
	}
}

// ---------------------------------------------------------------------------
// Device info
// ---------------------------------------------------------------------------

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	info, err := s.adb.DeviceInfo(serial)
	reply(w, info, err)
}

// ---------------------------------------------------------------------------
// Applications
// ---------------------------------------------------------------------------

func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	list, err := s.adb.Apps(serial, r.URL.Query().Get("kind"))
	reply(w, list, err)
}

func (s *Server) appAction(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Serial  string `json:"serial"`
		Action  string `json:"action"`
		Package string `json:"package"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.adb.AppAction(req.Serial, req.Action, req.Package)
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "apps/"+req.Action, req.Package+" "+out)
	reply(w, map[string]interface{}{"message": out}, err)
}

func tempDir() string { return filepath.Join(os.TempDir(), dataDirName) }

func saveUpload(r *http.Request, field string) (string, string, error) {
	if err := os.MkdirAll(tempDir(), 0o700); err != nil {
		return "", "", err
	}
	f, hdr, err := r.FormFile(field)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	name := filepath.Base(hdr.Filename)
	if name == "" || name == "." {
		return "", "", errors.New("无效文件名")
	}
	out, err := os.CreateTemp(tempDir(), "up-*"+filepath.Ext(name))
	if err != nil {
		return "", "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(f, 1<<30)); err != nil {
		return "", "", err
	}
	return out.Name(), name, nil
}

const maxAPKDownload = 1 << 30 // 1 GiB, enough for any real-world APK

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// httpErrorDetail keeps the useful part of a 4xx/5xx response body. Many APK
// mirrors explain "400" in HTML; showing only the status code made the old
// error message useless.
func httpErrorDetail(b []byte) string {
	s := htmlTagRE.ReplaceAllString(string(b), " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}

// downloadAPK fetches a direct APK link into a temporary file. It is shared by
// the "install from URL" flow and covered by api_test.go: a bare HTTP 400 from
// the remote server is now reported with the server's own explanation, and an
// HTML landing page is rejected before adb install sees it.
func downloadAPK(rawURL string) (local, name string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", errors.New("链接格式不正确，请输入完整的 http/https APK 下载地址")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", errors.New("仅支持 http/https 链接")
	}

	jar, _ := cookiejar.New(nil)
	cl := &http.Client{
		Timeout: 5 * time.Minute,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("重定向次数过多，请换一个直链")
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "+
		"(KHTML, like Gecko) PCWebADB/1.1.0 Safari/537.36")
	req.Header.Set("Accept", "application/vnd.android.package-archive,application/octet-stream,*/*")

	if err := os.MkdirAll(tempDir(), 0o700); err != nil {
		return "", "", err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if detail := httpErrorDetail(body); detail != "" {
			return "", "", fmt.Errorf("下载失败：HTTP %d（%s）", resp.StatusCode, detail)
		}
		return "", "", fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}

	name = filepath.Base(u.Path)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, e := mime.ParseMediaType(cd); e == nil {
			if fn := strings.TrimSpace(params["filename"]); fn != "" {
				name = filepath.Base(strings.ReplaceAll(fn, `"`, ""))
			}
		}
	}
	// A remote server controls Content-Disposition. Strip separators and
	// control characters before the name reaches logs or the UI.
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = "download.apk"
	}
	if !strings.EqualFold(filepath.Ext(name), ".apk") {
		name += ".apk"
	}

	f, err := os.CreateTemp(tempDir(), "dl-*.apk")
	if err != nil {
		return "", "", err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxAPKDownload))
	if err != nil {
		f.Close()
		return "", "", err
	}
	if written == 0 {
		f.Close()
		return "", "", errors.New("下载内容为空，请确认链接直接指向 APK 文件")
	}
	var magic [2]byte
	if _, err = f.ReadAt(magic[:], 0); err != nil {
		f.Close()
		return "", "", err
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", "", err
	}
	if magic != [2]byte{'P', 'K'} {
		body, _ := os.ReadFile(f.Name())
		limit := len(body)
		if limit > 240 {
			limit = 240
		}
		return "", "", fmt.Errorf("下载内容不是 APK（应为 ZIP 格式，实际开头：%s）。请确认链接直接指向 .apk 文件，而不是网页或需要登录的页面", httpErrorDetail(body[:limit]))
	}
	return f.Name(), name, nil
}

func (s *Server) appInstall(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	serial := r.FormValue("serial")
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	var local, name string
	var err error
	if link := strings.TrimSpace(r.FormValue("url")); link != "" {
		local, name, err = downloadAPK(link)
		if err != nil {
			reply(w, nil, err)
			return
		}
	} else {
		if err = r.ParseMultipartForm(1 << 30); err != nil {
			reply(w, nil, err)
			return
		}
		local, name, err = saveUpload(r, "file")
		if err != nil {
			reply(w, nil, err)
			return
		}
	}
	defer os.Remove(local)
	out, err := s.adb.DeviceText(serial, "install", "-r", "-d", local)
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "install", name+" "+out)
	reply(w, map[string]interface{}{"message": out}, err)
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = s.store.Get().DefaultFilesPath
	}
	list, err := s.adb.ListFiles(serial, path)
	reply(w, list, err)
}

func (s *Server) fileAction(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Serial string `json:"serial"`
		Action string `json:"action"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.adb.FileAction(req.Serial, req.Action, req.Path, req.Name)
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "files/"+req.Action, req.Path+" "+out)
	reply(w, map[string]interface{}{"message": out}, err)
}

func (s *Server) fileUpload(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		reply(w, nil, err)
		return
	}
	serial := r.FormValue("serial")
	dir := r.FormValue("path")
	if serial == "" || dir == "" {
		reply(w, nil, errors.New("缺少设备或目标目录"))
		return
	}
	if err := validateRemotePath(dir); err != nil {
		reply(w, nil, err)
		return
	}
	local, name, err := saveUpload(r, "file")
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer os.Remove(local)
	out, err := s.adb.DeviceText(serial, "push", local, joinRemote(dir, name))
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "push", name+" "+out)
	reply(w, map[string]interface{}{"message": out}, err)
}

func (s *Server) fileDownload(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	path := r.URL.Query().Get("path")
	if serial == "" || path == "" {
		reply(w, nil, errors.New("缺少设备或文件路径"))
		return
	}
	if err := validateRemotePath(path); err != nil {
		reply(w, nil, err)
		return
	}
	path = normalizeRemotePath(path)
	// `adb pull` on a directory fails after creating a zero-byte file, which
	// used to surface as HTTP 200 with an empty body - the browser then saved a
	// 0-byte download with the folder's name. Reject it explicitly instead.
	if s.adb.IsDir(serial, path) {
		reply(w, nil, errors.New("不能直接下载目录，请进入该目录后选择具体文件"))
		return
	}
	os.MkdirAll(tempDir(), 0o700)
	local, err := os.CreateTemp(tempDir(), "pull-*")
	if err != nil {
		reply(w, nil, err)
		return
	}
	local.Close()
	os.Remove(local.Name())
	defer os.Remove(local.Name())
	out, err := s.adb.DeviceText(serial, "pull", path, local.Name())
	if err != nil {
		reply(w, nil, err)
		return
	}
	f, err := os.Open(local.Name())
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = "读取远端文件失败：" + path
		}
		reply(w, nil, errors.New(msg))
		return
	}
	defer f.Close()
	if st, e := f.Stat(); e == nil && st.Size() == 0 {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = "远端文件为空或不可读：" + path
		}
		reply(w, nil, errors.New(msg))
		return
	}
	name := filepath.Base(path)
	if name == "." || name == "/" || name == "" {
		name = "download"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(name, `"`, "")))
	if st, e := f.Stat(); e == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	}
	io.Copy(w, f)
	s.log("info", "pull", path)
}

// ---------------------------------------------------------------------------
// Screenshot / logs / shell
// ---------------------------------------------------------------------------

func (s *Server) screenshot(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	buf, err := s.adb.Screenshot(serial)
	if err != nil {
		reply(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf)
}

var logFilterRE = regexp.MustCompile(`^[A-Za-z0-9_./:*\- ]+$`)

func logcatLinesParam(r *http.Request) string {
	v := strings.TrimSpace(r.URL.Query().Get("lines"))
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 100000 {
		return "200"
	}
	return v
}

func logcatFilterParam(r *http.Request) (string, error) {
	f := strings.TrimSpace(r.URL.Query().Get("filter"))
	if f == "" {
		return "", nil
	}
	if !logFilterRE.MatchString(f) {
		return "", errors.New("过滤表达式含非法字符")
	}
	return f, nil
}

func (s *Server) logcat(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	lines := logcatLinesParam(r)
	filter, err := logcatFilterParam(r)
	if err != nil {
		reply(w, nil, err)
		return
	}
	args := []string{"shell", "logcat", "-d", "-t", lines, "-v", "threadtime"}
	if filter != "" {
		args = append(args, filter)
	}
	out, err := s.adb.DeviceText(serial, args...)
	reply(w, out, err)
}

func (s *Server) logcatStream(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	serial := serialOf(r)
	if serial == "" {
		reply(w, nil, errors.New("请先选择设备"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		reply(w, nil, errors.New("当前服务器不支持流式输出"))
		return
	}
	filter, err := logcatFilterParam(r)
	if err != nil {
		reply(w, nil, err)
		return
	}
	args := withSerial(serial, "logcat", "-v", "threadtime")
	if filter != "" {
		args = append(args, filter)
	}
	cmd := exec.CommandContext(r.Context(), s.adb.Bin, args...)
	tuneCommand(cmd)
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		reply(w, nil, err)
		return
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		reply(w, nil, err)
		return
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Connection", "keep-alive")
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
		if r.Context().Err() != nil {
			return
		}
	}
}

func (s *Server) shell(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Serial  string `json:"serial"`
		Command string `json:"command"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.adb.Shell(req.Serial, req.Command)
	s.log(map[bool]string{true: "error", false: "info"}[err != nil], "shell", req.Command+" -> "+out)
	reply(w, map[string]interface{}{"output": out}, err)
}

func (s *Server) key(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Serial string `json:"serial"`
		Key    string `json:"key"`
		Text   string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	var out string
	var err error
	if req.Text != "" {
		out, err = s.adb.InputText(req.Serial, req.Text)
	} else {
		out, err = s.adb.InputKey(req.Serial, req.Key)
	}
	reply(w, map[string]interface{}{"message": out}, err)
}

// ---------------------------------------------------------------------------
// Screen mirror (delegated to scrcpy on the desktop)
// ---------------------------------------------------------------------------

func (s *Server) mirror(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Serial  string `json:"serial"`
		Action  string `json:"action"`
		Quality string `json:"quality"`
	}
	if err := decode(r, &req); err != nil {
		reply(w, nil, err)
		return
	}
	s.mirrorMu.Lock()
	defer s.mirrorMu.Unlock()
	if req.Action == "stop" {
		if c, ok := s.mirrors[req.Serial]; ok && c.Process != nil {
			c.Process.Kill()
			delete(s.mirrors, req.Serial)
		}
		reply(w, map[string]interface{}{"message": "已停止"}, nil)
		return
	}
	if c, ok := s.mirrors[req.Serial]; ok && c.Process != nil {
		reply(w, nil, errors.New("该设备已在镜像中"))
		return
	}
	bin, err := exec.LookPath("scrcpy")
	if err != nil {
		reply(w, nil, errors.New(mirrorHint))
		return
	}
	size := "1024"
	switch req.Quality {
	case "smooth":
		size = "640"
	case "high":
		size = "1920"
	}
	cmd := exec.Command(bin, "-s", req.Serial, "--max-size", size, "--window-title", appName+" · "+req.Serial)
	tuneCommand(cmd)
	if err := cmd.Start(); err != nil {
		reply(w, nil, err)
		return
	}
	s.mirrors[req.Serial] = cmd
	go func() {
		cmd.Wait()
		s.mirrorMu.Lock()
		delete(s.mirrors, req.Serial)
		s.mirrorMu.Unlock()
	}()
	s.log("info", "mirror", req.Serial+" quality="+req.Quality)
	reply(w, map[string]interface{}{"message": "已启动 scrcpy (" + size + ")"}, nil)
}

// ---------------------------------------------------------------------------
// Diagnostics / settings
// ---------------------------------------------------------------------------

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		reply(w, s.diag.List(), nil)
	case http.MethodDelete:
		s.diag.Clear()
		reply(w, []Diagnostic{}, nil)
	default:
		method(w, r, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		reply(w, s.store.Get(), nil)
	case http.MethodPut, http.MethodPost:
		var v Settings
		if err := decode(r, &v); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, s.store.Set(v), nil)
	default:
		method(w, r, http.MethodGet, http.MethodPut, http.MethodPost)
	}
}
