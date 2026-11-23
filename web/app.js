/* PC Web ADB (PWA) - frontend. Mirrors the skyadb (Android) feature layout:
   设备 / 详情 / 应用 / 文件 / 截图 / 日志 / Shell / 遥控器 / 镜像 / 诊断 / 设置 */
'use strict';

const ICONS = {
  devices: '<circle cx="7" cy="7" r="3"/><rect x="2" y="12" width="12" height="9" rx="2"/><path d="M17 6h4v15h-6"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7.5v.5"/>',
  apps: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  files: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m4 17 5-5 4 4 3-3 4 4"/>',
  logs: '<path d="M4 5h16M4 10h16M4 15h11M4 20h8"/>',
  shell: '<rect x="2" y="4" width="20" height="16" rx="2"/><path d="m6 9 3 3-3 3M12 15h5"/>',
  remote: '<rect x="6" y="2" width="12" height="20" rx="3"/><circle cx="12" cy="8" r="2.5"/><path d="M9 14h6M9 18h6"/>',
  mirror: '<rect x="2" y="4" width="20" height="13" rx="2"/><path d="M8 21h8M12 17v4"/>',
  diag: '<path d="M12 3 2 20h20z"/><path d="M12 9v5M12 17v.5"/>',
  settings: '<circle cx="12" cy="12" r="3.2"/><path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1-2.8 2.8-.1-.1a1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 0-1 1.5V21h-4v-.2a1.6 1.6 0 0 0-1-1.5 1.6 1.6 0 0 0-1.8.3l-.1.1-2.8-2.8.1-.1a1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H3v-4h.2a1.6 1.6 0 0 0 1.5-1 1.6 1.6 0 0 0-.3-1.8l-.1-.1 2.8-2.8.1.1a1.6 1.6 0 0 0 1.8.3 1.6 1.6 0 0 0 1-1.5V3h4v.2a1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3l.1-.1 2.8 2.8-.1.1a1.6 1.6 0 0 0-.3 1.8 1.6 1.6 0 0 0 1.5 1H21v4h-.2a1.6 1.6 0 0 0-1.4 1z"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/>',
  theme: '<path d="M12 3v2M12 19v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M3 12h2M19 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4"/><circle cx="12" cy="12" r="4"/>'
};

const VIEW_META = {
  devices: ['设备连接', '无线配对 · 手动连接 · 局域网发现 · 最近设备'],
  info: ['设备详情', '基础信息 · 快捷入口'],
  apps: ['应用管理', '列表 · 启动 / 停止 / 卸载 / 安装 APK'],
  files: ['文件管理', '目录浏览 · 上传 / 下载 / 重命名 / 删除'],
  screenshot: ['设备截图', '预览后再保存到本机'],
  logcat: ['系统日志', 'Logcat 快照与实时流'],
  shell: ['Shell', '在目标设备执行命令'],
  remote: ['虚拟遥控器', '按键注入 · 文本输入'],
  mirror: ['屏幕镜像', '调用 scrcpy 打开独立窗口'],
  diagnostics: ['诊断日志', '连接与操作异常记录'],
  settings: ['设置', '端口 · 超时 · 网段 · 主题 · 关于']
};

const S = {
  state: null, settings: null, devices: [], recent: [], serial: '', view: 'devices',
  apps: [], files: [], filesPath: '/sdcard', logSource: null, logLive: false, shotUrl: ''
};

const $ = (id) => document.getElementById(id);
const q = (sel, root = document) => root.querySelector(sel);
const qa = (sel, root = document) => Array.from(root.querySelectorAll(sel));

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function fmtSize(n) {
  if (!n) return '—';
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB';
  return (n / 1073741824).toFixed(2) + ' GB';
}
function toast(msg, kind = '') {
  const el = document.createElement('div');
  el.className = 'toast ' + kind;
  el.textContent = msg;
  $('toast').appendChild(el);
  setTimeout(() => el.remove(), 4200);
}
function ask(title, body) {
  return new Promise(resolve => {
    $('modalTitle').textContent = title;
    $('modalBody').textContent = body;
    $('modalBack').classList.remove('hidden');
    const done = (v) => { $('modalBack').classList.add('hidden'); resolve(v); };
    $('modalOk').onclick = () => done(true);
    $('modalCancel').onclick = () => done(false);
  });
}

async function api(path, opts = {}) {
  let res;
  try { res = await fetch(path, opts); }
  catch (e) { throw new Error('无法连接本地服务：' + e.message); }
  const ct = res.headers.get('content-type') || '';
  let body;
  if (ct.includes('application/json')) body = await res.json();
  else body = await res.text();
  if (!res.ok || (body && body.ok === false)) {
    throw new Error((body && (body.error || body.message)) || ('HTTP ' + res.status));
  }
  return (body && body.data !== undefined) ? body.data : body;
}
const jpost = (p, d) => api(p, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(d) });
const jput = (p, d) => api(p, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(d) });
const jdel = (p) => api(p, { method: 'DELETE' });

async function guard(fn, okMsg) {
  try {
    const r = await fn();
    if (okMsg) toast(okMsg, 'ok');
    return r;
  } catch (e) { toast(e.message, 'err'); return null; }
}
function needSerial() {
  if (!S.serial) { toast('请先在右上角选择一台设备', 'err'); return false; }
  return true;
}

/* ------------------------------ init ------------------------------------ */
function paintIcons() {
  qa('[data-ico]').forEach(el => {
    const p = ICONS[el.dataset.ico] || ICONS.info;
    el.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">' + p + '</svg>';
  });
}
function applyTheme(mode) {
  let m = mode || 'system';
  if (m === 'system') m = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  document.documentElement.dataset.theme = m;
}
async function init() {
  paintIcons();
  bindEvents();
  applyTheme('dark');
  try {
    S.state = await api('/api/state');
    S.settings = S.state.settings;
    $('adbVersionLabel').textContent = S.state.adbVersion || 'adb';
    $('connPort').placeholder = String(S.settings.defaultPort);
    $('scanPort').placeholder = String(S.settings.defaultPort);
    $('filesPath').value = S.settings.defaultFilesPath;
    fillSettings(S.settings);
    renderAbout(S.state);
    applyTheme(S.settings.theme);
  } catch (e) { toast(e.message, 'err'); }
  await refreshDevices();
  await loadRecent();
  setInterval(() => { if (S.view === 'devices') refreshDevices(); }, 8000);
}

function renderAbout(st) {
  $('aboutBox').innerHTML = [
    ['产品名称', esc(st.app) + ' ' + esc(st.version)],
    ['参考项目', 'github.com/sky22333/skyadb · GPL-3.0'],
    ['上游版本', esc(st.reference)],
    ['内置 adb', esc(st.adbVersion)],
    ['adb 路径', esc(st.adbPath)],
    ['配置目录', esc(st.configDir)],
    ['本程序许可', 'GPL-3.0-or-later（新增实现）；内置 adb 为 Android SDK Platform Tools（Apache-2.0 / Android SDK License）'],
    ['界面', '本地 HTTP 服务 + 浏览器渲染，仅监听 127.0.0.1'],
    ['运行日志', esc(st.logPath || '')]
  ].map(([k, v]) => `<div class="k">${k}</div><div class="v">${v}</div>`).join('');
}

/* --------------------------- device state ------------------------------- */
async function refreshDevices() {
  try {
    const list = await api('/api/devices');
    S.devices = list;
    const sel = $('deviceSelect');
    const prev = S.serial;
    sel.innerHTML = '<option value="">未选择设备</option>' + list.map(d => {
      const label = (d.model || d.product || d.serial) + (d.transport === 'usb' ? ' · USB' : ' · WiFi');
      return `<option value="${esc(d.serial)}">${esc(label)}</option>`;
    }).join('');
    if (list.some(d => d.serial === prev)) S.serial = prev;
    else S.serial = list.find(d => d.state === 'device')?.serial || '';
    sel.value = S.serial;
    updateStatus();
    renderDeviceList();
  } catch (e) { toast(e.message, 'err'); }
}
function updateStatus() {
  const d = S.devices.find(x => x.serial === S.serial);
  const dot = $('statusDot');
  dot.className = 'dot' + (!d ? '' : d.state === 'device' ? ' ok' : ' warn');
  $('viewSub').textContent = d ? `${d.serial} · ${d.state}` : '尚未连接设备';
}
function deviceStateLabel(st) {
  return { device: '已授权', unauthorized: '未授权', offline: '离线', recovery: '恢复中', nopermissions: '无权限' }[st] || st;
}
function deviceStateHint(d) {
  switch (d.state) {
    case 'unauthorized': return `【${d.serial}】设备未授权：请看设备屏幕上的「USB/网络调试授权」弹窗，勾选「始终允许」并点确定。没有弹窗时：开发者选项 → 撤销 USB 调试授权 → 点「重启 ADB 服务」→ 重新连接。`;
    case 'offline': return `【${d.serial}】设备离线：重新插拔数据线或检查网络，然后点上方「重启 ADB 服务」。`;
    case 'no permissions': return d.transport === 'usb'
      ? `【${d.serial}】USB 无权限：Windows 需安装设备驱动（Google USB Driver 或厂商助手自动装驱动）；Linux 需配置 udev 规则。`
      : `【${d.serial}】无权限：请重新连接。`;
    default: return '';
  }
}
function renderDeviceHint() {
  const box = $('deviceHint');
  if (!box) return;
  const bad = S.devices.filter(d => d.state && d.state !== 'device');
  const lines = bad.map(deviceStateHint);
  if (S.state && S.state.platform === 'windows' && S.devices.length && !S.devices.some(d => d.transport === 'usb' && d.state === 'device')) {
    lines.push('USB 设备未识别（Windows）：请确认手机已开启「USB 调试」并用原装数据线连接；首次使用需在手机上点「允许 USB 调试」。' +
      '若插入后毫无反应，多数是缺少 USB 驱动——安装 Google USB Driver，或运行一次小米/华为等官方助手让它自动装驱动，然后点「刷新」。');
  }
  if (!lines.length) { box.style.display = 'none'; box.innerHTML = ''; return; }
  box.style.display = '';
  box.innerHTML = lines.map(t => `<p>${esc(t)}</p>`).join('');
}
function renderDeviceList() {
  const box = $('deviceList');
  if (!S.devices.length) { box.innerHTML = '<div class="empty">暂无设备。可通过 USB 连接或手动输入 IP 连接。</div>'; renderDeviceHint(); return; }
  box.innerHTML = S.devices.map(d => `
    <div class="list-item" data-serial="${esc(d.serial)}">
      <div class="li-main"><strong>${esc(d.model || d.product || d.serial)}</strong>
        <small>${esc(d.serial)} · ${d.transport === 'usb' ? 'USB' : 'WiFi'}</small></div>
      <span class="state ${d.state === 'device' ? 'ok' : d.state === 'unauthorized' || d.state === 'offline' ? 'err' : 'warn'}">${esc(deviceStateLabel(d.state))}</span>
    </div>`).join('');
  renderDeviceHint();
}
async function loadRecent() {
  try { S.recent = await api('/api/recent'); renderRecent(); } catch (_) { }
}
function renderRecent() {
  const box = $('recentList');
  if (!S.recent.length) { box.innerHTML = '<div class="empty">暂无历史设备</div>'; return; }
  box.innerHTML = S.recent.map(r => `
    <div class="list-item" data-host="${esc(r.host)}" data-port="${r.port}">
      <div class="li-main"><strong>${esc(r.model || r.host)}</strong><small>${esc(r.host)}:${r.port} · ${esc((r.lastUsed || '').slice(0, 16).replace('T', ' '))}</small></div>
      <span class="state ok">一键连接</span>
    </div>`).join('');
}

/* ------------------------------ views ----------------------------------- */
function setView(name) {
  if (!VIEW_META[name]) return;
  stopLogStream();
  if (name !== 'mirror' && window.__stopPreview) window.__stopPreview();
  S.view = name;
  qa('.nav-item').forEach(b => b.classList.toggle('active', b.dataset.view === name));
  qa('.view').forEach(v => v.classList.toggle('active', v.dataset.view === name));
  $('viewTitle').textContent = VIEW_META[name][0];
  $('viewSub').textContent = S.serial ? `${S.serial} · ${VIEW_META[name][1]}` : VIEW_META[name][1];
  const loaders = { info: loadInfo, apps: loadApps, files: loadFiles, diagnostics: loadDiagnostics, settings: loadSettings };
  if (loaders[name]) loaders[name]();
}

/* ------------------------------ info ------------------------------------ */
const QUICK = [['应用管理', 'apps'], ['文件管理', 'files'], ['设备截图', 'screenshot'], ['系统日志', 'logcat'], ['Shell', 'shell'], ['虚拟遥控器', 'remote'], ['屏幕镜像', 'mirror'], ['诊断日志', 'diagnostics']];
function renderQuick() {
  $('quickGrid').innerHTML = QUICK.map(([t, v]) => `<button class="quick" data-go="${v}">${t}</button>`).join('');
}
async function loadInfo() {
  renderQuick();
  if (!S.serial) { $('infoGrid').innerHTML = '<div class="empty">选择设备后显示系统信息</div>'; return; }
  $('infoGrid').innerHTML = '<div class="empty">正在读取设备信息…</div>';
  const info = await guard(() => api('/api/info?serial=' + encodeURIComponent(S.serial)));
  if (!info) { $('infoGrid').innerHTML = '<div class="empty">读取失败</div>'; return; }
  const keys = Object.keys(info);
  $('infoGrid').innerHTML = keys.length ? keys.map(k => `<div class="info-cell"><span>${esc(k)}</span><strong>${esc(info[k])}</strong></div>`).join('') : '<div class="empty">未读取到属性</div>';
}

/* ------------------------------ apps ------------------------------------ */
async function loadApps() {
  if (!S.serial) { $('appsTable').querySelector('tbody').innerHTML = '<tr><td colspan="4" class="empty">请先选择设备</td></tr>'; return; }
  const kind = $('appsKind').value;
  const list = await guard(() => api(`/api/apps?serial=${encodeURIComponent(S.serial)}&kind=${kind}`));
  if (!list) return;
  S.apps = list;
  renderApps();
}
function renderApps() {
  const qv = $('appsSearch').value.trim().toLowerCase();
  const rows = S.apps.filter(a => !qv || a.package.toLowerCase().includes(qv));
  $('appsCount').textContent = rows.length + ' 个';
  const tbody = $('appsTable').querySelector('tbody');
  if (!rows.length) { tbody.innerHTML = '<tr><td colspan="4" class="empty">暂无应用</td></tr>'; return; }
  tbody.innerHTML = rows.map(a => `
    <tr data-pkg="${esc(a.package)}">
      <td class="mono">${esc(a.package)}<br><small class="muted">${esc(a.apkPath || '')}</small></td>
      <td>${a.kind === 'system' ? '系统' : '用户'}</td>
      <td><span class="state ${a.enabled ? 'ok' : 'warn'}">${a.enabled ? '已启用' : '已冻结'}</span></td>
      <td class="right"><div class="cell-actions">
        <button class="mini" data-act="launch">启动</button>
        <button class="mini" data-act="stop">停止</button>
        <button class="mini" data-act="${a.enabled ? 'disable' : 'enable'}">${a.enabled ? '冻结' : '启用'}</button>
        <button class="mini" data-act="export">APK 路径</button>
        <button class="mini danger" data-act="uninstall">卸载</button>
      </div></td></tr>`).join('');
}

/* ------------------------------ files ----------------------------------- */
async function loadFiles() {
  if (!S.serial) { $('filesTable').querySelector('tbody').innerHTML = '<tr><td colspan="5" class="empty">请先选择设备</td></tr>'; return; }
  const list = await guard(() => api(`/api/files?serial=${encodeURIComponent(S.serial)}&path=${encodeURIComponent(S.filesPath)}`));
  if (!list) return;
  S.files = list;
  renderFiles();
}
function renderFiles() {
  $('filesPath').value = S.filesPath;
  $('filesCount').textContent = S.files.length + ' 项';
  const tbody = $('filesTable').querySelector('tbody');
  const parent = S.filesPath.replace(/\/$/, '').split('/').slice(0, -1).join('/') || '/';
  let html = `<tr><td class="name-cell" data-up="1">📁 ..</td><td colspan="4" class="muted">返回上级 ${esc(parent)}</td></tr>`;
  html += S.files.map((f, i) => `
    <tr data-idx="${i}">
      <td class="name-cell" ${f.dir ? 'data-open="' + esc(f.path) + '"' : ''}>${f.dir ? '📁' : f.link ? '🔗' : '📄'} ${esc(f.name)}</td>
      <td class="mono">${esc(f.perms)}</td><td>${f.dir ? '文件夹' : fmtSize(f.size)}</td>
      <td class="muted">${esc(f.modTime)}</td>
      <td class="right"><div class="cell-actions">
        ${f.dir ? '' : `<a class="mini" href="/api/files/download?serial=${encodeURIComponent(S.serial)}&path=${encodeURIComponent(f.path)}" download="${esc(f.name)}">下载</a>`}
        <button class="mini" data-fact="rename">重命名</button>
        <button class="mini danger" data-fact="delete">删除</button>
      </div></td></tr>`).join('');
  tbody.innerHTML = html;
}

/* --------------------------- screenshot --------------------------------- */
async function takeShot() {
  if (!needSerial()) return;
  const url = `/api/screenshot?serial=${encodeURIComponent(S.serial)}&_=${Date.now()}`;
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error((await res.text()));
    const blob = await res.blob();
    if (S.shotUrl) URL.revokeObjectURL(S.shotUrl);
    S.shotUrl = URL.createObjectURL(blob);
    $('shotBox').innerHTML = `<img src="${S.shotUrl}" alt="设备截图">`;
    toast('截图已生成，可保存到本机', 'ok');
  } catch (e) { toast('截图失败：' + e.message, 'err'); }
}
function saveShot() {
  if (!S.shotUrl) { toast('请先截取屏幕', 'err'); return; }
  const a = document.createElement('a');
  a.href = S.shotUrl;
  a.download = 'pcwebadb-' + Date.now() + '.png';
  a.click();
}

/* ------------------------------ logcat ---------------------------------- */
async function fetchLog() {
  if (!needSerial()) return;
  const p = `/api/logcat?serial=${encodeURIComponent(S.serial)}&lines=${encodeURIComponent($('logLines').value || '200')}&filter=${encodeURIComponent($('logFilter').value)}`;
  const out = await guard(() => api(p));
  if (out != null) $('logOutput').textContent = out || '（无日志）';
}
function stopLogStream() {
  if (S.logSource) { S.logSource.close(); S.logSource = null; }
  S.logLive = false;
  $('btnLogLive').textContent = '开始实时';
  $('logState').textContent = '未连接';
}
function toggleLogLive() {
  if (S.logLive) { stopLogStream(); toast('已停止实时日志'); return; }
  if (!needSerial()) return;
  const p = `/api/logcat/stream?serial=${encodeURIComponent(S.serial)}&filter=${encodeURIComponent($('logFilter').value)}`;
  $('logOutput').textContent = '';
  const es = new EventSource(p);
  let buf = '';
  es.onmessage = (e) => {
    buf += e.data + '\n';
    if (buf.length > 4000) {
      $('logOutput').textContent += buf;
      buf = '';
      $('logOutput').scrollTop = $('logOutput').scrollHeight;
    }
  };
  es.onerror = () => { $('logState').textContent = '连接已断开'; stopLogStream(); };
  S.logSource = es; S.logLive = true;
  $('btnLogLive').textContent = '停止实时';
  $('logState').textContent = '实时中';
}

/* ------------------------------ shell ----------------------------------- */
const PRESETS = ['getprop ro.product.model', 'pm list packages -3', 'wm size', 'dumpsys battery', 'df -h /data', 'ip -o -4 addr show', 'uptime'];
function renderPresets() {
  $('shellPresets').innerHTML = PRESETS.map(c => `<button class="chip-btn" data-cmd="${esc(c)}">${esc(c)}</button>`).join('');
}
async function runShell(cmd) {
  if (!needSerial()) return;
  const out = $('shellOutput');
  out.textContent += `\n$ ${cmd}\n`;
  const r = await guard(() => jpost('/api/shell', { serial: S.serial, command: cmd }));
  out.textContent += r ? (r.output || '（无输出）') + '\n' : '执行失败\n';
  out.scrollTop = out.scrollHeight;
}

/* ------------------------------ remote ---------------------------------- */
const DPAD = [['', ''], ['↑', 'DPAD_UP'], [''], ['←', 'DPAD_LEFT'], ['OK', 'DPAD_CENTER'], ['→', 'DPAD_RIGHT'], [''], ['↓', 'DPAD_DOWN'], ['']];
const MEDIA = [['返回', 'BACK'], ['主页', 'HOME'], ['菜单', 'MENU'], ['多任务', 'APP_SWITCH'], ['播放/暂停', 'MEDIA_PLAY_PAUSE'], ['音量+', 'VOLUME_UP'], ['音量-', 'VOLUME_DOWN'], ['电源', 'POWER']];
const MORE = [['亮度+', 'BRIGHTNESS_UP'], ['亮度-', 'BRIGHTNESS_DOWN'], ['上一曲', 'MEDIA_PREVIOUS'], ['下一曲', 'MEDIA_NEXT'], ['静音', 'VOLUME_MUTE'], ['相机', 'CAMERA'], ['唤醒', 'WAKEUP'], ['睡眠', 'SLEEP']];
function renderRemote() {
  $('dpad').innerHTML = DPAD.map(([t, k]) => k ? `<button class="key ${k === 'DPAD_CENTER' ? 'center' : ''}" data-key="${k}">${t}</button>` : '<button class="key blank"></button>').join('');
  $('mediaRow').innerHTML = MEDIA.map(([t, k]) => `<button class="btn small" data-key="${k}">${t}</button>`).join('');
  $('keyChips').innerHTML = MORE.map(([t, k]) => `<button class="chip-btn" data-key="${k}">${t}</button>`).join('');
}
async function sendKey(k) {
  if (!needSerial()) return;
  await guard(() => jpost('/api/key', { serial: S.serial, key: k }));
}

/* --------------------------- diagnostics -------------------------------- */
async function loadDiagnostics() {
  const list = await guard(() => api('/api/diagnostics'));
  if (!list) return;
  const tbody = $('diagTable').querySelector('tbody');
  tbody.innerHTML = list.length ? list.slice().reverse().map(d => `
    <tr><td class="mono">${esc(d.time)}</td>
      <td><span class="state ${d.level === 'error' ? 'err' : d.level === 'warn' ? 'warn' : 'ok'}">${esc(d.level)}</span></td>
      <td class="mono">${esc(d.op)}</td><td>${esc(d.msg)}</td></tr>`).join('')
    : '<tr><td colspan="4" class="empty">暂无记录</td></tr>';
}

/* ------------------------------ settings -------------------------------- */
function fillSettings(st) {
  $('setPort').value = st.defaultPort;
  $('setConnectTimeout').value = st.connectTimeout;
  $('setCommandTimeout').value = st.commandTimeout;
  $('setConcurrency').value = st.scanConcurrency;
  $('setRanges').value = st.scanRanges || '';
  $('setFilesPath').value = st.defaultFilesPath;
  $('setMirrorQuality').value = st.mirrorQuality;
  $('setTheme').value = st.theme;
  $('setAdbPath').value = st.adbPath || '';
  $('mirrorQuality').value = st.mirrorQuality;
}
async function loadSettings() { try { S.settings = await api('/api/settings'); fillSettings(S.settings); } catch (_) { } }
async function saveSettings() {
  const s = Object.assign({}, S.settings, {
    defaultPort: +$('setPort').value,
    connectTimeout: +$('setConnectTimeout').value,
    commandTimeout: +$('setCommandTimeout').value,
    scanConcurrency: +$('setConcurrency').value,
    scanRanges: $('setRanges').value.trim(),
    defaultFilesPath: $('setFilesPath').value.trim() || '/sdcard',
    mirrorQuality: $('setMirrorQuality').value,
    theme: $('setTheme').value,
    adbPath: $('setAdbPath').value.trim()
  });
  const r = await guard(() => jput('/api/settings', s), '设置已保存');
  if (r) { S.settings = r; applyTheme(r.theme); }
}

/* ------------------------------ events ---------------------------------- */
function bindEvents() {
  qa('.nav-item').forEach(b => b.onclick = () => setView(b.dataset.view));
  $('deviceSelect').onchange = e => { S.serial = e.target.value; updateStatus(); const l = { info: loadInfo, apps: loadApps, files: loadFiles }; if (l[S.view]) l[S.view](); };
  $('btnRefresh').onclick = refreshDevices;
  $('btnTheme').onclick = async () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
    await guard(() => jput('/api/settings', Object.assign({}, S.settings, { theme: next })));
    if (S.settings) S.settings.theme = next;
  };
  $('quickGrid').addEventListener('click', e => { const b = e.target.closest('[data-go]'); if (b) setView(b.dataset.go); });
  $('deviceList').addEventListener('click', e => {
    const it = e.target.closest('[data-serial]'); if (!it) return;
    S.serial = it.dataset.serial; $('deviceSelect').value = S.serial; updateStatus(); toast('已选择 ' + S.serial, 'ok');
  });
  $('recentList').addEventListener('click', async e => {
    const it = e.target.closest('[data-host]'); if (!it) return;
    $('connHost').value = it.dataset.host; $('connPort').value = it.dataset.port;
    await doConnect();
  });

  $('btnConnect').onclick = doConnect;
  $('connHost').onkeydown = e => { if (e.key === 'Enter') doConnect(); };
  $('btnPair').onclick = () => guard(async () => {
    const r = await jpost('/api/pair', { host: $('pairHost').value.trim(), port: +$('pairPort').value, code: $('pairCode').value.trim() });
    toast('配对结果：' + (r.message || '完成'), 'ok');
    $('scanResult').textContent = r.message || '';
  });
  $('btnDisconnect').onclick = async () => {
    if (!needSerial()) return;
    if (!await ask('断开连接', `确定断开 ${S.serial} ？`)) return;
    await guard(async () => { await jpost('/api/disconnect', { serial: S.serial }); await refreshDevices(); }, '已断开');
  };
  $('btnScan').onclick = async () => {
    const btn = $('btnScan'); const old = btn.textContent;
    btn.textContent = '扫描中…'; btn.disabled = true; $('scanResult').textContent = '正在扫描网段，请稍候…';
    try {
      const list = await api('/api/discover', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ range: $('scanRange').value.trim(), port: +$('scanPort').value || 0 }) });
      $('scanResult').textContent = list.length ? list.map(f => `${f.host}:${f.port}  ${f.connected ? '✓ 已连接' : '✗ ' + f.message}`).join('\n') : '未发现开放 ADB 端口的设备';
      await refreshDevices();
      toast(`扫描完成，发现 ${list.length} 台设备`, 'ok');
    } catch (e) { $('scanResult').textContent = e.message; toast(e.message, 'err'); }
    finally { btn.textContent = old; btn.disabled = false; }
  };
  $('btnRefreshDev').onclick = refreshDevices;
  $('btnRecoverAdb').onclick = () => guard(async () => {
    const r = await jpost('/api/recover', {});
    (r.steps || []).forEach(s => toast(s, 'ok'));
    await refreshDevices();
    return r.message;
  }, 'adb 服务已重启');
  $('btnClearRecent').onclick = async () => { await guard(jdel.bind(null, '/api/recent')); await loadRecent(); };

  $('btnRefreshInfo').onclick = loadInfo;
  $('btnAppsRefresh').onclick = loadApps;
  $('appsKind').onchange = loadApps;
  $('appsSearch').oninput = renderApps;
  $('appsTable').querySelector('tbody').addEventListener('click', async e => {
    const b = e.target.closest('[data-act]'); if (!b) return;
    const pkg = b.closest('tr').dataset.pkg, act = b.dataset.act;
    if (!needSerial()) return;
    if (act === 'uninstall' && !await ask('确认卸载应用？', `将从目标设备卸载 ${pkg}。`)) return;
    if (act === 'disable' && !await ask('确认冻结应用？', `冻结系统应用可能影响桌面、设置或播放功能。\n${pkg}`)) return;
    const r = await guard(() => jpost('/api/apps/action', { serial: S.serial, action: act, package: pkg }), '操作已执行');
    if (r && r.message) toast(String(r.message).slice(0, 200));
    if (['uninstall', 'enable', 'disable'].includes(act)) loadApps();
  });
  $('btnInstallApk').onclick = async () => {
    if (!needSerial()) return;
    const f = $('apkFile').files[0]; if (!f) { toast('请先选择 APK 文件', 'err'); return; }
    const fd = new FormData(); fd.append('serial', S.serial); fd.append('file', f);
    toast('正在安装，请稍候…');
    const r = await guard(() => api('/api/apps/install', { method: 'POST', body: fd }), '安装命令已执行');
    if (r) toast(String(r.message || '').slice(0, 240));
    loadApps();
  };
  $('btnInstallUrl').onclick = async () => {
    if (!needSerial()) return;
    const u = $('apkUrl').value.trim(); if (!u) { toast('请输入下载链接', 'err'); return; }
    const fd = new FormData(); fd.append('serial', S.serial); fd.append('url', u);
    toast('正在下载并安装…');
    const r = await guard(() => api('/api/apps/install', { method: 'POST', body: fd }), '安装命令已执行');
    if (r) toast(String(r.message || '').slice(0, 240));
    loadApps();
  };

  $('btnFilesGo').onclick = () => { S.filesPath = $('filesPath').value.trim() || '/sdcard'; loadFiles(); };
  $('filesPath').onkeydown = e => { if (e.key === 'Enter') { S.filesPath = $('filesPath').value.trim() || '/sdcard'; loadFiles(); } };
  $('btnFilesUp').onclick = () => {
    const p = S.filesPath.replace(/\/$/, '');
    S.filesPath = p.split('/').slice(0, -1).join('/') || '/';
    loadFiles();
  };
  $('btnFilesRefresh').onclick = loadFiles;
  $('btnUpload').onclick = async () => {
    if (!needSerial()) return;
    const f = $('uploadFile').files[0]; if (!f) { toast('请先选择文件', 'err'); return; }
    const fd = new FormData(); fd.append('serial', S.serial); fd.append('path', S.filesPath); fd.append('file', f);
    toast('正在上传…');
    const r = await guard(() => api('/api/files/upload', { method: 'POST', body: fd }), '上传完成');
    if (r) $('scanResult').textContent = r.message || '';
    loadFiles();
  };
  $('btnMkdir').onclick = async () => {
    if (!needSerial()) return;
    const name = prompt('新建文件夹名称'); if (!name) return;
    await guard(() => jpost('/api/files/action', { serial: S.serial, action: 'mkdir', path: S.filesPath, name }), '已创建');
    loadFiles();
  };
  $('filesTable').querySelector('tbody').addEventListener('click', async e => {
    const up = e.target.closest('[data-up]');
    if (up) { $('btnFilesUp').click(); return; }
    const open = e.target.closest('[data-open]');
    if (open) { S.filesPath = open.dataset.open; loadFiles(); return; }
    const b = e.target.closest('[data-fact]'); if (!b) return;
    const i = b.closest('tr').dataset.idx, f = S.files[i]; if (!f || !needSerial()) return;
    if (b.dataset.fact === 'delete') {
      if (!await ask('确认删除？', `将删除 ${f.path}，此操作不可恢复。`)) return;
      await guard(() => jpost('/api/files/action', { serial: S.serial, action: 'delete', path: f.path }), '已删除');
    } else {
      const name = prompt('新名称', f.name); if (!name || name === f.name) return;
      await guard(() => jpost('/api/files/action', { serial: S.serial, action: 'rename', path: f.path, name }), '已重命名');
    }
    loadFiles();
  });

  $('btnShot').onclick = takeShot;
  $('btnShotSave').onclick = saveShot;
  $('btnLogFetch').onclick = fetchLog;
  $('btnLogLive').onclick = toggleLogLive;
  $('btnLogCopy').onclick = () => navigator.clipboard.writeText($('logOutput').textContent).then(() => toast('日志已复制', 'ok')).catch(e => toast(e.message, 'err'));

  renderPresets();
  $('shellCmd').onkeydown = e => { if (e.key === 'Enter') { const c = $('shellCmd').value.trim(); if (c) { $('shellCmd').value = ''; runShell(c); } } };
  $('btnShellRun').onclick = () => { const c = $('shellCmd').value.trim(); if (c) { $('shellCmd').value = ''; runShell(c); } };
  $('btnShellClear').onclick = () => $('shellOutput').textContent = '$ 等待命令…';
  $('shellPresets').addEventListener('click', e => { const b = e.target.closest('[data-cmd]'); if (b) runShell(b.dataset.cmd); });

  renderRemote();
  document.addEventListener('click', async e => {
    const b = e.target.closest('[data-key]'); if (!b) return;
    await sendKey(b.dataset.key);
  });
  $('btnInputText').onclick = () => guard(() => jpost('/api/key', { serial: S.serial, text: $('inputText').value }), '已发送');

  $('btnMirrorStart').onclick = () => guard(async () => {
    if (!needSerial()) return;
    const r = await jpost('/api/mirror', { serial: S.serial, action: 'start', quality: $('mirrorQuality').value });
    $('mirrorStatus').textContent = r.message || '已启动';
    toast(r.message || '已启动', 'ok');
  });
  $('btnMirrorStop').onclick = () => guard(async () => {
    if (!needSerial()) return;
    const r = await jpost('/api/mirror', { serial: S.serial, action: 'stop' });
    $('mirrorStatus').textContent = r.message || '已停止';
  });

  // 内置预览：串行拉帧，上一帧加载完成后再排下一次，避免请求堆积。
  let previewOn = false;
  async function previewTick() {
    if (!previewOn) return;
    const img = $('mirrorPreview');
    const url = `/api/screenshot?serial=${encodeURIComponent(S.serial)}&_=${Date.now()}`;
    await new Promise(res => { img.onload = res; img.onerror = res; img.src = url; });
    if (!previewOn) return;
    const ok = img.naturalWidth > 0;
    $('previewStatus').textContent = ok
      ? `预览中 · ${img.naturalWidth}×${img.naturalHeight} · ${new Date().toLocaleTimeString()}`
      : '抓帧失败，请确认设备已授权并重新选择设备';
    setTimeout(previewTick, Math.max(300, +$('previewInterval').value || 1500));
  }
  $('btnPreviewStart').onclick = () => {
    if (!needSerial()) return;
    previewOn = true;
    $('previewStatus').textContent = '正在抓取首帧…';
    previewTick();
  };
  $('btnPreviewStop').onclick = () => { previewOn = false; $('previewStatus').textContent = '已停止'; };
  window.__stopPreview = () => { previewOn = false; };

  $('btnDiagRefresh').onclick = loadDiagnostics;
  $('btnDiagClear').onclick = async () => { await guard(jdel.bind(null, '/api/diagnostics')); loadDiagnostics(); };
  $('btnSettingsSave').onclick = saveSettings;
}

async function doConnect() {
  const host = $('connHost').value.trim();
  const port = +$('connPort').value || S.settings.defaultPort;
  if (!host) { toast('请输入设备 IP 地址', 'err'); return; }
  const r = await guard(async () => {
    const out = await jpost('/api/connect', { host, port });
    await refreshDevices();
    await loadRecent();
    if (out.serial) { S.serial = out.serial; $('deviceSelect').value = out.serial; updateStatus(); }
    return out;
  }, '连接成功');
  if (r && r.message) toast(String(r.message).slice(0, 200));
}

window.addEventListener('DOMContentLoaded', init);
