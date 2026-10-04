import { test, expect, openDashboard, navigateDashboard, openSettings, navigateSettings, actionResponse } from '../support/fixtures.js';

test('manual model prompts cancel/trim and both model lists preserve custom selection', async ({ page }) => {
  await openSettings(page);
  for (const [index, id] of [[0, 'sModel'], [1, 'sCodex']]) {
    const manual = page.getByRole('button', { name: 'Isi Model Manual', exact: true }).nth(index);
    const before = await page.locator(`#${id}`).inputValue();
    page.once('dialog', d => d.dismiss());
    await manual.click();
    await expect(page.locator(`#${id}`)).toHaveValue(before);
    page.once('dialog', d => d.accept(`  custom-ui-${index}  `));
    await manual.click();
    await expect(page.locator(`#${id}`)).toHaveValue(`custom-ui-${index}`);
    await actionResponse(page, '/api/models', 'POST', () => page.getByRole('button', { name: 'Cek Daftar Model', exact: true }).nth(index).click());
    await expect(page.locator(`#${id}`)).toHaveValue(`custom-ui-${index}`);
  }
  await page.locator('#sWire').selectOption('responses');
  await page.locator('#sCodexWire').selectOption('messages');
  const saved = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Endpoint & Perilaku Agen' }).click());
  expect(saved.body.config).toMatchObject({ llm_model: 'custom-ui-0', codex_model: 'custom-ui-1', llm_wire_api: 'responses', codex_wire_api: 'messages' });
});

test('staff search, form reset, reloads and settings section navigation', async ({ page, request }) => {
  await request.post('/api/staff', { data: { number: '628120009876', name: 'Searchable UI staff', role: 'noc_senior', active: true } });
  await openSettings(page);
  for (const link of await page.locator('.settings-nav a').all()) {
    await link.click();
    await expect(link).toHaveAttribute('aria-current', 'page');
  }
  await navigateSettings(page, '#staffCard');
  await page.locator('#staffFilter').fill('not-present');
  await expect(page.locator('#staffFilterEmpty')).toContainText('Tidak ada staf');
  await page.locator('#staffFilter').fill('searchable');
  await expect(page.locator('#staffList tbody tr:visible')).toHaveCount(1);
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByRole('button', { name: 'Bersihkan Form' }).click();
  await expect(page.locator('#stfNumber')).toHaveValue('');
  await expect(page.locator('#stfPin')).toHaveValue('');
  for (const [hash, label, route] of [['#staffCard', 'Muat ulang staf', '/api/staff'], ['#teamCard', 'Muat ulang mode', '/api/team/metrics'], ['#recipesCard', '↻ Muat Ulang Resep', '/api/recipes']]) {
    await navigateSettings(page, hash);
    await actionResponse(page, route, 'GET', () => page.getByRole('button', { name: label, exact: true }).click());
  }
});

test('router fields persist, unsaved test blocked, remove cancel then accept', async ({ page, request }) => {
  await openSettings(page, '#integrations');
  await page.getByRole('button', { name: /Tambah Router MikroTik/ }).click();
  const row = page.locator('#mkList .mk-row');
  for (const [field, value] of Object.entries({ name: 'UI router', host: '127.0.0.1', port: '1', user: 'ui-router', pass: 'synthetic-router-only' })) {
    await row.locator(`[data-k=${field}]`).fill(value);
  }
  await row.locator('[data-k=tls]').selectOption('true');
  await page.getByRole('button', { name: 'Tes Koneksi Router', exact: true }).click();
  await expect(page.locator('#intMsg')).toContainText('Simpan integrasi terlebih dahulu');
  const saved = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Integrasi', exact: true }).click());
  expect(saved.sent.mikrotik_routers[0]).toMatchObject({ name: 'UI router', host: '127.0.0.1', port: 1, tls: true });
  const checked = await actionResponse(page, '/api/mikrotik/check', 'GET', () => page.getByRole('button', { name: 'Tes Koneksi Router', exact: true }).click());
  expect(checked.body.ok).toBe(false); // Loopback port 1; never a real router.
  page.once('dialog', d => d.dismiss());
  await page.getByRole('button', { name: 'Hapus Router', exact: true }).click();
  await expect(row).toHaveCount(1);
  page.once('dialog', d => d.accept());
  await page.getByRole('button', { name: 'Hapus Router', exact: true }).click();
  await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Integrasi', exact: true }).click());
  expect((await (await request.get('/api/status')).json()).config.mikrotik_count).toBe(0);
});

test('shared shell links reveal panels; reload sessions and clear probe validation', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await openDashboard(page);
  await page.locator('.workspace-nav').getByRole('link', { name: 'AI Diagnostics' }).click();
  await expect(page.locator('#diagnosisPanel')).toBeVisible();
  await expect(page).toHaveURL(/#diagnosisPanel$/);
  await expect(page.locator('#caseQueueTitle')).toBeHidden();
  await page.locator('.trace-disclosure summary').click();
  await page.locator('#probeRun').click();
  await expect(page.locator('#probeHint')).toContainText('Pilih tool');
  await page.getByRole('button', { name: 'Bersihkan', exact: true }).click();
  await expect(page.locator('#steps li')).toHaveCount(0);
  await page.locator('.workspace-nav').getByRole('link', { name: 'WhatsApp', exact: true }).click();
  await expect(page.locator('#adminDrawer')).toHaveAttribute('open', '');
  await page.locator('.workspace-nav').getByRole('link', { name: 'Riwayat & sesi' }).click();
  await actionResponse(page, '/api/sesi', 'GET', () => page.getByRole('button', { name: '↻ Muat Ulang Sesi' }).click());
  await page.locator('.workspace-nav').getByRole('link', { name: 'Kasus operasi' }).click();
  await expect(page).toHaveURL(/#caseQueueTitle$/);
});

test('SSE rendering uses actual nonstream Go report, not invented report fields', async ({ page, request }) => {
  // API-02 separately exercises real Go SSE success. Only transport is synthesized here.
  const r = await request.post('/api/ask', { data: { query: 'Halo, selamat pagi', stream: false } });
  expect(r.status()).toBe(200);
  const report = await r.json();
  expect(report).toMatchObject({ id: expect.any(String), answer: expect.any(String), engine: expect.any(String), steps: expect.any(Array) });
  await page.route('**/api/ask', route => route.fulfill({ contentType: 'text/event-stream', body:
    report.steps.map(step => `event: step\r\ndata: ${JSON.stringify(step)}\r\n\r\n`).join('') + `event: report\r\ndata: ${JSON.stringify(report)}` }));
  await openDashboard(page);
  await page.locator('#toggleDiagnosis').click();
  await page.locator('#q').fill('Halo, selamat pagi');
  await page.locator('#tgt').fill('ui-customer');
  const pending = page.waitForRequest('**/api/ask');
  await page.locator('#run').click();
  expect((await pending).postDataJSON()).toEqual({ query: 'Halo, selamat pagi', target: 'ui-customer', stream: true });
  await expect(page.locator('#answer')).toHaveText(report.answer);
  await expect(page.locator('#answer b')).toHaveCount(0);
  await expect(page.locator('#run')).toBeEnabled();
  await navigateDashboard(page, '#historySection');
  await expect(page.locator('#hist')).toBeVisible();
  await expect(page.locator('#hist')).toContainText(report.engine);
});
