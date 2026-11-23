//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl

#include <dlfcn.h>
#include <stdlib.h>

// cgo does not define _GNU_SOURCE, so RTLD_DEFAULT may be unavailable.
#ifndef RTLD_DEFAULT
#define RTLD_DEFAULT ((void *)0)
#endif

// SkyADB PC's Linux desktop shell.
//
// GTK3 / WebKitGTK are loaded at RUNTIME with dlopen rather than linked at
// build time. That keeps the build free of GTK dev packages / pkg-config and,
// more importantly, lets the program fall back to a browser app window on
// systems without WebKitGTK instead of failing to start at all.
//
// Closing the window now HIDES it and leaves the Go service running in the
// background, with a GtkStatusIcon in the notification area. GTK is strictly
// single-threaded, so Go-side requests (signals, /api/raise) only set flags
// here; the g_timeout_add callback runs on the GTK main thread and acts on
// them. No GTK call is ever made from a Go goroutine.

struct sky_api {
    int   (*init_check)(int *, char ***);
    void *(*window_new)(int);
    void  (*window_set_title)(void *, const char *);
    void  (*window_set_default_size)(void *, int, int);
    void  (*window_set_icon_name)(void *, const char *);
    void  (*window_set_wmclass)(void *, const char *, const char *);
    void  (*window_present)(void *);
    void  (*container_add)(void *, void *);
    void  (*widget_show_all)(void *);
    void  (*widget_show)(void *);
    void  (*widget_hide)(void *);
    void  (*widget_destroy)(void *);
    void  (*main)(void);
    void  (*main_quit)(void);
    void *(*webview_new)(void);
    void  (*webview_load_uri)(void *, const char *);
    void *(*webview_get_settings)(void *);
    void  (*settings_set_dev_extras)(void *, int);
    unsigned long (*signal_connect_data)(void *, const char *, void (*)(void), void *, void *, int);
    unsigned int  (*timeout_add)(unsigned int, int (*)(void *), void *);
    void  (*set_prgname)(const char *);
    void  (*set_program_class)(const char *);

    void *(*status_icon_new)(void);
    void  (*status_icon_set_from_file)(void *, const char *);
    void  (*status_icon_set_from_icon_name)(void *, const char *);
    void  (*status_icon_set_tooltip_text)(void *, const char *);
    void  (*status_icon_set_title)(void *, const char *);
    void  (*status_icon_set_visible)(void *, int);
    void *(*menu_new)(void);
    void *(*menu_item_new_with_label)(const char *);
    void *(*separator_menu_item_new)(void);
    void  (*menu_shell_append)(void *, void *);
    // 4th argument is GtkMenuPositionFunc; a void* keeps the cast simple
    // because we always pass NULL.
    void  (*menu_popup)(void *, void *, void *, void *, void *, unsigned int, unsigned int);
};

static struct sky_api A;
static volatile int sky_stop = 0;
static volatile int sky_show = 0;
static void *sky_win = NULL;
static void *sky_icon = NULL;
static void *sky_menu = NULL;

#define LOAD(handle, member, sym) A.member = (__typeof__(A.member))dlsym(handle, sym)

static void sky_request_stop(void) { sky_stop = 1; }
static void sky_request_show(void) { sky_show = 1; }

// Runs on the GTK main thread every 200 ms.
static int sky_check_events(void *unused) {
    (void)unused;
    if (sky_stop) { if (A.main_quit) A.main_quit(); return 0; }
    if (sky_show) {
        sky_show = 0;
        if (sky_win) {
            if (A.widget_show_all) A.widget_show_all(sky_win);
            if (A.window_present) A.window_present(sky_win);
        }
    }
    return 1;
}

// delete-event must return TRUE: the window is hidden, NOT destroyed, so the
// WebKit view, the loopback service and the tray icon keep running.
static int sky_on_delete(void *widget, void *event, void *data) {
    (void)widget; (void)event; (void)data;
    if (A.widget_hide && sky_win) A.widget_hide(sky_win);
    return 1;
}

static void sky_on_destroy(void *widget, void *data) {
    (void)widget; (void)data;
    sky_stop = 1;
    if (A.main_quit) A.main_quit();
}

static void sky_tray_activate(void *icon, void *data) {
    (void)icon; (void)data;
    sky_show = 1;
}

static void sky_tray_popup(void *icon, unsigned int button, unsigned int t, void *data) {
    (void)icon; (void)data;
    if (sky_menu && A.menu_popup)
        A.menu_popup(sky_menu, NULL, NULL, NULL, NULL, button, t);
}

static void sky_menu_show(void *item, void *data) {
    (void)item; (void)data;
    sky_show = 1;
}

static void sky_menu_quit(void *item, void *data) {
    (void)item; (void)data;
    sky_stop = 1;
    if (A.main_quit) A.main_quit();
}

static int sky_tray_setup(const char *icon_file, const char *icon_name) {
    if (!A.status_icon_new || !A.status_icon_set_visible || !A.signal_connect_data ||
        !A.menu_new || !A.menu_item_new_with_label || !A.menu_shell_append ||
        !A.widget_show || !A.menu_popup) return 0;

    void *icon = A.status_icon_new();
    if (!icon) return 0;
    // Prefer the PNG written from go:embed: it works even when the .deb (and
    // therefore the hicolor icon theme entry) is not installed.
    if (icon_file && *icon_file && A.status_icon_set_from_file)
        A.status_icon_set_from_file(icon, icon_file);
    else if (A.status_icon_set_from_icon_name)
        A.status_icon_set_from_icon_name(icon, icon_name);
    if (A.status_icon_set_tooltip_text)
        A.status_icon_set_tooltip_text(icon, "PC Web ADB (PWA) · 后台运行中（右键菜单可退出）");
    if (A.status_icon_set_title) A.status_icon_set_title(icon, "PC Web ADB");
    A.status_icon_set_visible(icon, 1);

    void *menu = A.menu_new();
    if (menu) {
        void *show = A.menu_item_new_with_label("显示主界面");
        A.menu_shell_append(menu, show);
        A.signal_connect_data(show, "activate", (void (*)(void))sky_menu_show, NULL, NULL, 0);
        A.widget_show(show);
        if (A.separator_menu_item_new) {
            void *sep = A.separator_menu_item_new();
            A.menu_shell_append(menu, sep);
            A.widget_show(sep);
        }
        void *quit = A.menu_item_new_with_label("退出 PC Web ADB");
        A.menu_shell_append(menu, quit);
        A.signal_connect_data(quit, "activate", (void (*)(void))sky_menu_quit, NULL, NULL, 0);
        A.widget_show(quit);
    }
    A.signal_connect_data(icon, "activate", (void (*)(void))sky_tray_activate, NULL, NULL, 0);
    A.signal_connect_data(icon, "popup-menu", (void (*)(void))sky_tray_popup, NULL, NULL, 0);
    sky_icon = icon;
    sky_menu = menu;
    return 1;
}

// Returns 1 when every mandatory symbol was resolved.
static int sky_libs_ready(void) {
    if (A.webview_new) return 1;
    void *gtk = dlopen("libgtk-3.so.0", RTLD_LAZY | RTLD_GLOBAL);
    void *wk  = dlopen("libwebkit2gtk-4.0.so.37", RTLD_LAZY | RTLD_GLOBAL);
    if (!wk) wk = dlopen("libwebkit2gtk-4.1.so.0", RTLD_LAZY | RTLD_GLOBAL);
    if (!wk) wk = dlopen("libwebkit2gtk-4.0.so.36", RTLD_LAZY | RTLD_GLOBAL);
    void *gobj = dlopen("libgobject-2.0.so.0", RTLD_LAZY | RTLD_GLOBAL);
    void *glib = dlopen("libglib-2.0.so.0", RTLD_LAZY | RTLD_GLOBAL);
    if (!gtk || !wk || !gobj || !glib) return 0;

    LOAD(gtk, init_check, "gtk_init_check");
    LOAD(gtk, window_new, "gtk_window_new");
    LOAD(gtk, window_set_title, "gtk_window_set_title");
    LOAD(gtk, window_set_default_size, "gtk_window_set_default_size");
    LOAD(gtk, window_set_icon_name, "gtk_window_set_icon_name");
    LOAD(gtk, window_set_wmclass, "gtk_window_set_wmclass");
    LOAD(gtk, window_present, "gtk_window_present");
    LOAD(gtk, container_add, "gtk_container_add");
    LOAD(gtk, widget_show_all, "gtk_widget_show_all");
    LOAD(gtk, widget_show, "gtk_widget_show");
    LOAD(gtk, widget_hide, "gtk_widget_hide");
    LOAD(gtk, widget_destroy, "gtk_widget_destroy");
    LOAD(gtk, main, "gtk_main");
    LOAD(gtk, main_quit, "gtk_main_quit");

    LOAD(gtk, status_icon_new, "gtk_status_icon_new");
    LOAD(gtk, status_icon_set_from_file, "gtk_status_icon_set_from_file");
    LOAD(gtk, status_icon_set_from_icon_name, "gtk_status_icon_set_from_icon_name");
    LOAD(gtk, status_icon_set_tooltip_text, "gtk_status_icon_set_tooltip_text");
    LOAD(gtk, status_icon_set_title, "gtk_status_icon_set_title");
    LOAD(gtk, status_icon_set_visible, "gtk_status_icon_set_visible");
    LOAD(gtk, menu_new, "gtk_menu_new");
    LOAD(gtk, menu_item_new_with_label, "gtk_menu_item_new_with_label");
    LOAD(gtk, separator_menu_item_new, "gtk_separator_menu_item_new");
    LOAD(gtk, menu_shell_append, "gtk_menu_shell_append");
    LOAD(gtk, menu_popup, "gtk_menu_popup");

    LOAD(wk, webview_new, "webkit_web_view_new");
    LOAD(wk, webview_load_uri, "webkit_web_view_load_uri");
    LOAD(wk, webview_get_settings, "webkit_web_view_get_settings");
    LOAD(wk, settings_set_dev_extras, "webkit_settings_set_enable_developer_extras");

    LOAD(gobj, signal_connect_data, "g_signal_connect_data");
    LOAD(glib, timeout_add, "g_timeout_add");
    LOAD(glib, set_prgname, "g_set_prgname");

    // Optional: makes the taskbar dock the launcher icon to this window.
    LOAD(gtk, set_program_class, "gdk_set_program_class");

    if (!A.init_check || !A.window_new || !A.window_set_title || !A.container_add ||
        !A.widget_show_all || !A.main || !A.main_quit || !A.webview_new ||
        !A.webview_load_uri || !A.signal_connect_data || !A.timeout_add) return 0;
    return 1;
}

// Returns 0 = user quit from the tray / signal, 1 = GTK/WebKit unavailable,
// 2 = no display / gtk_init failed, 3 = mandatory symbol missing.
static int sky_run_window(const char *url, const char *title,
                          const char *icon, const char *icon_file,
                          const char *wmclass, int width, int height) {
    if (!sky_libs_ready()) return 1;

    if (A.set_prgname) A.set_prgname("pcwebadb");
    if (A.set_program_class) A.set_program_class(wmclass);

    char *args[] = {(char *)"pcwebadb", NULL};
    int argc = 1;
    char **argv = args;
    if (!A.init_check(&argc, &argv)) return 2;

    void *win = A.window_new(0); // GTK_WINDOW_TOPLEVEL
    if (!win) return 3;
    sky_win = win;
    A.window_set_title(win, title);
    if (A.window_set_default_size) A.window_set_default_size(win, width, height);
    if (A.window_set_icon_name) A.window_set_icon_name(win, icon);
    // Must be set before the window is realized for StartupWMClass to match.
    if (A.window_set_wmclass) A.window_set_wmclass(win, wmclass, wmclass);

    void *view = A.webview_new();
    if (!view) { if (A.widget_destroy) A.widget_destroy(win); return 3; }
    if (A.webview_get_settings && A.settings_set_dev_extras)
        A.settings_set_dev_extras(A.webview_get_settings(view), 0);

    A.signal_connect_data(win, "delete-event", (void (*)(void))sky_on_delete, NULL, NULL, 0);
    A.signal_connect_data(win, "destroy", (void (*)(void))sky_on_destroy, NULL, NULL, 0);
    sky_tray_setup(icon_file, icon);
    A.container_add(win, view);
    A.webview_load_uri(view, url);
    A.widget_show_all(win);

    // Poll the Go-side flags so signals and a second launch can show/quit the
    // window from the GTK thread without any cross-thread GTK calls.
    sky_stop = 0;
    sky_show = 0;
    A.timeout_add(200, sky_check_events, NULL);
    A.main();
    return 0;
}
*/
import "C"

import (
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

// trayIconPath materialises the embedded PNG so the status icon does not
// depend on the .deb being installed (icon-theme lookup). It is tiny (1.4 KB)
// and re-used across launches.
func trayIconPath() string {
	if len(trayIconPNG) == 0 {
		return ""
	}
	p := filepath.Join(configDir(), "tray-icon.png")
	if st, err := os.Stat(p); err == nil && st.Size() == int64(len(trayIconPNG)) {
		return p
	}
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(p, trayIconPNG, 0o600); err != nil {
		return ""
	}
	return p
}

// runNativeWindow blocks on the GTK main loop until the user quits from the
// tray menu or a termination signal arrives. It must run on the process's
// initial OS thread: GDK assumes its X11 connection belongs to the main thread.
func runNativeWindow(url string) int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cURL := C.CString(url)
	cTitle := C.CString(appName + " · ADB 设备管理")
	cIcon := C.CString("pcwebadb")
	cIconFile := C.CString(trayIconPath())
	cClass := C.CString(wmClass)
	defer func() {
		C.free(unsafe.Pointer(cURL))
		C.free(unsafe.Pointer(cTitle))
		C.free(unsafe.Pointer(cIcon))
		C.free(unsafe.Pointer(cIconFile))
		C.free(unsafe.Pointer(cClass))
	}()
	return int(C.sky_run_window(cURL, cTitle, cIcon, cIconFile, cClass, 1280, 820))
}

// requestNativeStop lets the signal handler unblock the GTK main loop.
func requestNativeStop() { C.sky_request_stop() }

// requestNativeShow is called by /api/raise (and by a second launch through
// the running instance's HTTP endpoint). It only sets a flag.
func requestNativeShow() { C.sky_request_show() }
