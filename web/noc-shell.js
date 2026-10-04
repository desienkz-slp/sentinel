/* Shared presentation only. No API, credentials or backend configuration. */
(() => {
  const settings = location.pathname.includes('settings');
  document.body.classList.add('app-page', settings ? 'settings-page' : 'dashboard-page');
  const icon = (name) => {
    const paths = {
      overview: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
      cases: '<path d="M8 4H5a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2h-3"/><rect x="8" y="2" width="8" height="4" rx="1"/><path d="M7 11h10M7 16h6"/>',
      ai: '<path d="m12 3 2.8 6.2L21 12l-6.2 2.8L12 21l-2.8-6.2L3 12l6.2-2.8Z"/>',
      link: '<path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2"/>',
      history: '<path d="M3 11a9 9 0 1 1 3 8M3 4v7h7M12 7v5l3 2"/>',
      settings: '<path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3"/><circle cx="16" cy="17" r="3"/>'
    };
    return `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name]}</svg>`;
  };
  const nav = document.createElement('aside');
  nav.className = 'app-sidebar';
  nav.innerHTML = `<a class="sidebar-brand" href="/" aria-label="NOC Sentinel beranda"><img src="/logo.png" alt="" width="34" height="34"><span>NOC Sentinel<small>OPERATIONS CONSOLE</small></span></a>
    <div class="nav-caption">WORKSPACE</div><nav class="workspace-nav" aria-label="Navigasi utama">
    <a href="/" ${!settings ? 'class="is-active" aria-current="page"' : ''}>${icon('overview')}<span>Overview</span></a>
    <a href="/#caseQueueTitle">${icon('cases')}<span>Kasus operasi</span></a>
    <a href="/#diagnosisPanel" data-open-diagnosis>${icon('ai')}<span>AI Diagnostics</span></a>
    <a href="/#adminDrawer" data-open-wa>${icon('link')}<span>WhatsApp</span></a>
    <a href="/#historySection">${icon('history')}<span>Riwayat & sesi</span></a>
    <a href="/settings.html" ${settings ? 'class="is-active" aria-current="page"' : ''}>${icon('settings')}<span>Pengaturan</span></a>
    </nav><div class="sidebar-footer"><span class="sidebar-mark">N</span><div>Precision Dark<small>Network operations workspace</small></div></div>`;
  document.body.prepend(nav);
  const skip = document.createElement('a');
  skip.href = '#mainContent'; skip.className = 'skip-link'; skip.textContent = 'Lewati navigasi';
  document.body.prepend(skip);
  const dashboardRoutes = {
    '': ['overview', 'Operations overview', 'Ringkasan kasus, alert dependensi, dan postur operasi.'],
    '#caseQueueTitle': ['cases', 'Kasus operasi', 'Cari, filter, dan telusuri timeline serta bukti setiap kasus.'],
    '#diagnosisPanel': ['diagnostics', 'AI Diagnostics', 'Investigasi keluhan, telusuri langkah AI, dan jalankan probe.'],
    '#adminDrawer': ['whatsapp', 'WhatsApp', 'Kelola gateway, sesi, pairing, pesan uji, dan proteksi nomor.'],
    '#historySection': ['history', 'Riwayat & sesi', 'Tinjau percakapan aktif dan riwayat investigasi.']
  };
  const settingRoutes = {
    '#conversation': ['conversation', 'reasoning'], '#agent': ['agent'],
    '#integrations': ['integrations'], '#teamCard': ['teamCard'],
    '#staffCard': ['staffCard'], '#policyCard': ['policyCard'], '#recipesCard': ['recipesCard']
  };
  function activate(link, active) {
    link.classList.toggle('is-active', active);
    if (active) link.setAttribute('aria-current', 'page'); else link.removeAttribute('aria-current');
  }
  function route(focus = false) {
    let hash = location.hash;
    // The skip link is an accessibility anchor, not a view change.
    if (hash === '#mainContent') {
      if (document.body.dataset.view || document.body.dataset.settingsView) return;
      hash = ''; // A direct skip-anchor URL still initializes an exclusive view.
    }
    if (settings) {
      if (hash === '#reasoning') hash = '#conversation';
      if (!settingRoutes[hash]) hash = '#conversation';
      const ids = settingRoutes[hash];
      document.querySelectorAll('.settings-content > section').forEach(el => el.hidden = !ids.includes(el.id));
      const save = document.getElementById('aiSaveBar');
      if (save) save.hidden = !['#conversation', '#agent'].includes(hash);
      document.querySelectorAll('.settings-nav a').forEach(a => activate(a, a.hash === hash));
      document.body.dataset.settingsView = hash.slice(1);
      const active = document.querySelector('.settings-nav a[aria-current]');
      document.title = `${active?.textContent.trim().replace(/^\d+\s*/, '') || 'Pengaturan'} — NOC Sentinel`;
    } else {
      if (!dashboardRoutes[hash]) hash = '';
      const [view, title, description] = dashboardRoutes[hash];
      document.body.dataset.view = view;
      document.querySelectorAll('[data-view]').forEach(el => {
        if (el !== document.body) el.hidden = el.dataset.view !== view;
      });
      document.querySelector('.cockpit-grid').hidden = view === 'history';
      document.querySelector('.operations-side').hidden = !['overview','whatsapp'].includes(view);
      document.getElementById('viewTitle').textContent = title;
      document.getElementById('viewEyebrow').textContent = 'WORKSPACE / ' + title.toUpperCase();
      document.getElementById('viewDescription').textContent = description;
      document.getElementById('toggleDiagnosis').hidden = view === 'diagnostics';
      if (view === 'whatsapp') document.getElementById('adminDrawer').open = true;
      document.title = `${title} — NOC Sentinel`;
      nav.querySelectorAll('.workspace-nav a').forEach(a => activate(a, a.pathname === '/' && a.hash === hash));
      // A modal from the previous view must never obscure a new destination.
      const detail = document.getElementById('caseDetail');
      if (detail?.open) detail.close();
      document.getElementById('blModal')?.classList.add('hidden');
    }
    if (focus) {
      const heading = document.querySelector('.page-heading h1');
      heading?.setAttribute('tabindex', '-1');
      heading?.focus({preventScroll:true});
      window.scrollTo({top:0, behavior:'instant'});
    }
  }
  window.addEventListener('hashchange', () => route(true));
  // Push state only for in-document navigation; modified clicks keep native behavior.
  document.addEventListener('click', e => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = e.target.closest('a[href]');
    if (!a || a.target || a.hasAttribute('download')) return;
    const url = new URL(a.href, location.href);
    const samePage = url.origin === location.origin && (settings ? url.pathname === location.pathname : ['/', '/index.html'].includes(url.pathname));
    const known = settings ? !!settingRoutes[url.hash] || url.hash === '#reasoning' : Object.hasOwn(dashboardRoutes, url.hash);
    if (!samePage || !known) return;
    e.preventDefault();
    if (location.href !== url.href) history.pushState(null, '', url);
    route(true);
  });
  window.addEventListener('popstate', () => route(true));
  route();
  document.querySelectorAll('.hint[id], .status[id]').forEach(el => { el.setAttribute('role','status'); el.setAttribute('aria-live','polite'); });
})();
