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
  const reveal = () => {
    if (location.hash === '#adminDrawer') document.getElementById('adminDrawer')?.setAttribute('open', '');
    if (location.hash === '#diagnosisPanel') {
      document.getElementById('diagnosisPanel')?.classList.remove('hidden');
      const toggle = document.getElementById('toggleDiagnosis');
      if (toggle) { toggle.setAttribute('aria-expanded', 'true'); toggle.textContent = 'Tutup AI Diagnostics'; }
      document.querySelector('.cockpit-grid')?.classList.add('with-diagnosis');
    }
  };
  window.addEventListener('hashchange', reveal);
  nav.addEventListener('click', e => {
    if (e.target.closest('[data-open-diagnosis],[data-open-wa]') && !settings) setTimeout(reveal, 0);
  });
  reveal();
  document.querySelectorAll('.hint[id], .status[id]').forEach(el => { el.setAttribute('role','status'); el.setAttribute('aria-live','polite'); });
})();
