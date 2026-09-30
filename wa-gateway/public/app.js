// AI-NOC WhatsApp Gateway Management UI Client
let pollTimer = null;
let currentStatus = null;
let qrCountdown = 45;
let qrCountdownTimer = null;

document.addEventListener('DOMContentLoaded', () => {
  initClock();
  setupEventListeners();
  startPolling();
});

// ---------------- Clock ----------------
function initClock() {
  const clockEl = document.getElementById('live-clock');
  function update() {
    const now = new Date();
    // Format to Asia/Jakarta (WIB)
    const timeStr = now.toLocaleTimeString('en-GB', { timeZone: 'Asia/Jakarta' });
    clockEl.textContent = `${timeStr} WIB`;
  }
  update();
  setInterval(update, 1000);
}

// ---------------- Event Listeners ----------------
function setupEventListeners() {
  document.getElementById('btn-reconnect').addEventListener('click', handleReconnect);
  document.getElementById('btn-logout').addEventListener('click', handleLogout);
  document.getElementById('btn-refresh-qr').addEventListener('click', handleRefreshQR);

  // Simulation Runner
  document.getElementById('btn-run-sim').addEventListener('click', runSimulation);
  document.getElementById('sim-query-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') runSimulation();
  });

  // Quick Query Tags
  document.querySelectorAll('.quick-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const q = btn.getAttribute('data-query');
      document.getElementById('sim-query-input').value = q;
      runSimulation();
    });
  });
}

// ---------------- Status & Data Polling ----------------
function startPolling() {
  fetchStatus();
  fetchLogs();
  pollTimer = setInterval(() => {
    fetchStatus();
    fetchLogs();
  }, 2500);
}

async function fetchStatus() {
  try {
    const res = await fetch('/api/whatsapp/status');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    renderStatus(data);
  } catch (err) {
    console.warn('[Status] Poll failed:', err.message);
  }
}

function renderStatus(data) {
  const status = data.status || 'DISCONNECTED';
  currentStatus = status;

  const badgeEl = document.getElementById('badge-status');
  const textEl = document.getElementById('text-status');
  const phoneEl = document.getElementById('display-phone');
  const nameEl = document.getElementById('display-name');
  const sessionEl = document.getElementById('display-session');
  const connectedAtEl = document.getElementById('display-connected-at');
  const uptimeEl = document.getElementById('display-uptime');
  const qrContainer = document.getElementById('qr-container');

  sessionEl.textContent = data.session || 'ai-noc';
  uptimeEl.textContent = formatUptime(data.uptime_seconds);

  // Update Status Badge
  badgeEl.className = 'status-badge';
  if (status === 'CONNECTED') {
    badgeEl.classList.add('badge-connected');
    textEl.textContent = 'CONNECTED';
    phoneEl.textContent = formatPhone(data.phone) || 'Unknown Number';
    nameEl.textContent = data.name || 'AI-NOC Sentinel';
    connectedAtEl.textContent = data.connected_at ? formatTimeAgo(data.connected_at) : 'Active';

    // Hide QR Card when connected
    qrContainer.classList.add('hide');
    stopQRCountdown();
  } else if (status === 'CONNECTING') {
    badgeEl.classList.add('badge-connecting');
    textEl.textContent = 'CONNECTING...';
    phoneEl.textContent = 'Menghubungkan ke server...';
    qrContainer.classList.remove('hide');
  } else if (status === 'AUTH_REQUIRED') {
    badgeEl.classList.add('badge-auth');
    textEl.textContent = 'SCAN QR REQUIRED';
    phoneEl.textContent = 'Belum terhubung';
    qrContainer.classList.remove('hide');
    fetchQR();
  } else {
    badgeEl.classList.add('badge-disconnected');
    textEl.textContent = 'DISCONNECTED';
    phoneEl.textContent = 'Terputus';
    nameEl.textContent = 'AI-NOC Sentinel';
    connectedAtEl.textContent = 'Not connected';
    qrContainer.classList.remove('hide');
  }

  // Render Webhook Telemetry
  const dotWebhook = document.getElementById('dot-webhook');
  const valWebhook = document.getElementById('val-webhook');
  dotWebhook.className = 'status-indicator-dot';
  if (data.webhook === 'healthy') {
    dotWebhook.classList.add('green');
    valWebhook.textContent = 'HEALTHY (200 OK)';
  } else if (data.webhook === 'failing') {
    dotWebhook.classList.add('red');
    valWebhook.textContent = 'FAILING / RETRYING';
  } else {
    valWebhook.textContent = 'STANDBY';
  }

  // Render Last Inbound Message
  if (data.last_incoming) {
    document.getElementById('last-in-time').textContent = formatTimeAgo(data.last_incoming.timestamp);
    document.getElementById('last-in-sender').textContent = `From: ${data.last_incoming.sender_name || data.last_incoming.sender}`;
    document.getElementById('last-in-body').textContent = data.last_incoming.message || '(empty)';
  }

  // Render Last Outbound Message
  if (data.last_outgoing) {
    document.getElementById('last-out-time').textContent = formatTimeAgo(data.last_outgoing.timestamp);
    document.getElementById('last-out-to').textContent = `To: ${data.last_outgoing.to}`;
    document.getElementById('last-out-body').textContent = data.last_outgoing.message || '(empty)';
  }
}

// ---------------- QR Code Handling ----------------
async function fetchQR() {
  try {
    const res = await fetch('/api/whatsapp/qr');
    if (!res.ok) return;
    const data = await res.json();

    const qrImg = document.getElementById('qr-image');
    const loadingOverlay = document.getElementById('qr-loading');
    const timerText = document.getElementById('qr-timer-text');
    const progressFill = document.getElementById('qr-progress-fill');

    if (data.available && data.qr) {
      qrImg.src = data.qr;
      loadingOverlay.classList.add('hide');

      qrCountdown = data.expires_in || 45;
      timerText.textContent = `Refresh in ${qrCountdown}s`;
      progressFill.style.width = `${(qrCountdown / 45) * 100}%`;

      startQRCountdown();
    } else {
      loadingOverlay.classList.remove('hide');
    }
  } catch (err) {
    console.warn('[QR] Failed to fetch QR:', err.message);
  }
}

function startQRCountdown() {
  if (qrCountdownTimer) clearInterval(qrCountdownTimer);
  qrCountdownTimer = setInterval(() => {
    qrCountdown--;
    const timerText = document.getElementById('qr-timer-text');
    const progressFill = document.getElementById('qr-progress-fill');

    if (qrCountdown <= 0) {
      clearInterval(qrCountdownTimer);
      timerText.textContent = 'Expired. Refreshing...';
      progressFill.style.width = '0%';
      fetchQR();
    } else {
      timerText.textContent = `Refresh in ${qrCountdown}s`;
      progressFill.style.width = `${(qrCountdown / 45) * 100}%`;
    }
  }, 1000);
}

function stopQRCountdown() {
  if (qrCountdownTimer) clearInterval(qrCountdownTimer);
  qrCountdownTimer = null;
}

// ---------------- Actions ----------------
async function handleReconnect() {
  try {
    const btn = document.getElementById('btn-reconnect');
    btn.disabled = true;
    btn.textContent = 'Reconnecting...';

    const res = await fetch('/api/whatsapp/reconnect', { method: 'POST' });
    const data = await res.json();
    console.log('[Reconnect]', data);
  } catch (err) {
    alert(`Reconnect error: ${err.message}`);
  } finally {
    const btn = document.getElementById('btn-reconnect');
    btn.disabled = false;
    btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/></svg> Reconnect`;
    fetchStatus();
  }
}

async function handleLogout() {
  if (!confirm('Yakin ingin logout session WhatsApp ini? Sesi akan dihapus dan diperlukan scan QR ulang.')) {
    return;
  }

  try {
    const btn = document.getElementById('btn-logout');
    btn.disabled = true;
    btn.textContent = 'Logging out...';

    const res = await fetch('/api/whatsapp/logout', { method: 'POST' });
    const data = await res.json();
    console.log('[Logout]', data);
  } catch (err) {
    alert(`Logout error: ${err.message}`);
  } finally {
    const btn = document.getElementById('btn-logout');
    btn.disabled = false;
    btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/></svg> Logout`;
    fetchStatus();
  }
}

async function handleRefreshQR() {
  document.getElementById('qr-loading').classList.remove('hide');
  await handleReconnect();
  setTimeout(fetchQR, 2000);
}

// ---------------- Interactive Simulation Runner ----------------
async function runSimulation() {
  const inputEl = document.getElementById('sim-query-input');
  const query = inputEl.value.trim();
  if (!query) return;

  const btnText = document.getElementById('btn-sim-text');
  const spinner = document.getElementById('sim-spinner');
  const btn = document.getElementById('btn-run-sim');
  const resultBox = document.getElementById('sim-result-container');
  const resultText = document.getElementById('sim-result-text');
  const intentBadge = document.getElementById('sim-result-intent');

  btn.disabled = true;
  btnText.textContent = 'Mendiagnosa...';
  spinner.classList.remove('hide');

  try {
    const res = await fetch('/api/whatsapp/simulate-query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query })
    });

    const data = await res.json();
    resultBox.classList.remove('hide');

    if (data.error) {
      intentBadge.textContent = 'ERROR';
      resultText.textContent = `Gagal menjalankan diagnosa:\n${data.error}`;
    } else {
      intentBadge.textContent = `INTENT: ${data.intent || 'DETECTED'}`;
      resultText.textContent = data.report || JSON.stringify(data, null, 2);
    }
  } catch (err) {
    resultBox.classList.remove('hide');
    intentBadge.textContent = 'NETWORK ERROR';
    resultText.textContent = `Request failed: ${err.message}`;
  } finally {
    btn.disabled = false;
    btnText.textContent = 'Kirim & Diagnosa';
    spinner.classList.add('hide');
    fetchStatus();
  }
}

// ---------------- Logs Terminal ----------------
async function fetchLogs() {
  try {
    const res = await fetch('/api/whatsapp/logs');
    if (!res.ok) return;
    const data = await res.json();
    renderLogs(data.logs || []);
  } catch (err) {
    // Non-blocking
  }
}

function renderLogs(logs) {
  const terminal = document.getElementById('logs-terminal');
  if (!logs || logs.length === 0) return;

  terminal.innerHTML = '';
  logs.slice(0, 30).forEach(l => {
    const row = document.createElement('div');
    row.className = 'log-line';

    const ts = document.createElement('span');
    ts.className = 'log-ts';
    ts.textContent = `[${l.timestamp.split('T')[1].split('.')[0]}] `;

    const lvl = document.createElement('span');
    lvl.className = `log-lvl-${(l.level || 'info').toLowerCase()}`;
    lvl.textContent = `[${l.level}] `;

    const msg = document.createElement('span');
    msg.className = 'log-msg';
    msg.textContent = l.message;

    row.appendChild(ts);
    row.appendChild(lvl);
    row.appendChild(msg);
    terminal.appendChild(row);
  });
}

// ---------------- Helpers ----------------
function formatPhone(phone) {
  if (!phone) return null;
  const clean = String(phone).replace(/[^0-9]/g, '');
  if (clean.startsWith('62')) {
    return `+62 ${clean.substring(2, 5)}-${clean.substring(5, 9)}-${clean.substring(9)}`;
  }
  return `+${clean}`;
}

function formatUptime(seconds) {
  if (!seconds) return '--';
  const m = Math.floor(seconds / 60);
  const h = Math.floor(m / 60);
  if (h > 0) return `${h}h ${m % 60}m`;
  return `${m}m ${Math.floor(seconds % 60)}s`;
}

function formatTimeAgo(isoString) {
  if (!isoString) return '--';
  try {
    const diffMs = Date.now() - new Date(isoString).getTime();
    const sec = Math.floor(diffMs / 1000);
    if (sec < 60) return `${sec} detik lalu`;
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min} menit lalu`;
    const hr = Math.floor(min / 60);
    return `${hr} jam lalu`;
  } catch (e) {
    return isoString;
  }
}
