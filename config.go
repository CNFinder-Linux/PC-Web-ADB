package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const dataDirName = "pcwebadb"

// Settings mirrors the "设置" screen of the SkyADB reference app,
// extended with PC-specific options (scan concurrency, custom adb binary).
type Settings struct {
	DefaultPort      int    `json:"defaultPort"`
	ConnectTimeout   int    `json:"connectTimeout"` // milliseconds
	CommandTimeout   int    `json:"commandTimeout"` // seconds
	ScanRanges       string `json:"scanRanges"`
	ScanConcurrency  int    `json:"scanConcurrency"`
	MirrorQuality    string `json:"mirrorQuality"`
	Theme            string `json:"theme"`
	AdbPath          string `json:"adbPath"`
	DefaultFilesPath string `json:"defaultFilesPath"`
}

func defaultSettings() Settings {
	return Settings{
		DefaultPort:      5555,
		ConnectTimeout:   400,
		CommandTimeout:   20,
		ScanRanges:       "",
		ScanConcurrency:  64,
		MirrorQuality:    "balanced",
		Theme:            "system",
		AdbPath:          "",
		DefaultFilesPath: "/sdcard",
	}
}

// RecentDevice is the PC equivalent of SkyADB's "最近设备" list.
type RecentDevice struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Serial   string `json:"serial"`
	Model    string `json:"model"`
	LastUsed string `json:"lastUsed"`
}

type persistState struct {
	Settings Settings       `json:"settings"`
	Recent   []RecentDevice `json:"recent"`
}

// Store persists settings + recent devices under the data dir (see
// dataDirName: $XDG_CONFIG_HOME/pcwebadb on Unix, %APPDATA%\PC Web ADB on Windows).
type Store struct {
	mu   sync.Mutex
	path string
	data persistState
}

// configDir is implemented per platform in platform_unix.go (XDG) and
// platform_windows.go (%APPDATA%\PC Web ADB).

func NewStore() *Store {
	s := &Store{path: filepath.Join(configDir(), "config.json")}
	s.data = persistState{Settings: defaultSettings(), Recent: []RecentDevice{}}
	if b, err := os.ReadFile(s.path); err == nil {
		var loaded persistState
		if json.Unmarshal(b, &loaded) == nil {
			base := defaultSettings()
			// Fill zero values from defaults so older config files stay valid.
			raw, _ := json.Marshal(loaded.Settings)
			cur, _ := json.Marshal(base)
			var curMap, rawMap map[string]interface{}
			json.Unmarshal(cur, &curMap)
			json.Unmarshal(raw, &rawMap)
			for k, v := range rawMap {
				if v != nil && v != "" && v != float64(0) {
					curMap[k] = v
				}
			}
			merged, _ := json.Marshal(curMap)
			json.Unmarshal(merged, &base)
			s.data.Settings = normalizeSettings(base)
			if loaded.Recent != nil {
				s.data.Recent = loaded.Recent
			}
		}
	}
	return s
}

func (s *Store) save() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}

func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Settings
}

// normalizeSettings is the single source of truth for setting ranges. It is
// applied both when saving through the API and when loading an existing
// config.json, so a hand-edited/out-of-range value can never hang an adb call.
func normalizeSettings(v Settings) Settings {
	if v.DefaultPort < 1 || v.DefaultPort > 65535 {
		v.DefaultPort = 5555
	}
	if v.ConnectTimeout < 50 || v.ConnectTimeout > 20000 {
		v.ConnectTimeout = 400
	}
	if v.CommandTimeout < 1 || v.CommandTimeout > 600 {
		v.CommandTimeout = 20
	}
	if v.ScanConcurrency < 1 || v.ScanConcurrency > 512 {
		v.ScanConcurrency = 64
	}
	if v.MirrorQuality == "" {
		v.MirrorQuality = "balanced"
	}
	if v.Theme == "" {
		v.Theme = "system"
	}
	if v.DefaultFilesPath == "" {
		v.DefaultFilesPath = "/sdcard"
	}
	return v
}

func (s *Store) Set(v Settings) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	v = normalizeSettings(v)
	s.data.Settings = v
	s.save()
	return v
}

func (s *Store) Recent() []RecentDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RecentDevice, len(s.data.Recent))
	copy(out, s.data.Recent)
	return out
}

func (s *Store) AddRecent(d RecentDevice) []RecentDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.LastUsed = time.Now().Format(time.RFC3339)
	out := []RecentDevice{d}
	for _, x := range s.data.Recent {
		if x.Host == d.Host && x.Port == d.Port {
			continue
		}
		out = append(out, x)
	}
	if len(out) > 20 {
		out = out[:20]
	}
	s.data.Recent = out
	s.save()
	// Copy while still holding the lock. Calling s.Recent() here would try to
	// lock the same non-reentrant mutex and deadlock the whole server - which
	// is exactly what happened on the first connect from the UI.
	res := make([]RecentDevice, len(out))
	copy(res, out)
	return res
}

func (s *Store) ClearRecent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Recent = []RecentDevice{}
	s.save()
}

// ---------------------------------------------------------------------------
// Diagnostics: an in-memory ring buffer mirroring SkyADB's 诊断日志 screen.
// ---------------------------------------------------------------------------

type Diagnostic struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Op    string `json:"op"`
	Msg   string `json:"msg"`
}

type Diagnostics struct {
	mu      sync.Mutex
	entries []Diagnostic
}

func (d *Diagnostics) Add(level, op, msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = append(d.entries, Diagnostic{
		Time:  time.Now().Format("15:04:05.000"),
		Level: level, Op: op, Msg: msg,
	})
	if len(d.entries) > 500 {
		d.entries = d.entries[len(d.entries)-500:]
	}
	// Tee into the persistent run log; the in-memory ring is wiped on exit,
	// which makes GUI crashes undiagnosable.
	appLogf(level, "%s: %s", op, msg)
}

func (d *Diagnostics) List() []Diagnostic {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Diagnostic, len(d.entries))
	copy(out, d.entries)
	return out
}

func (d *Diagnostics) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = nil
}
