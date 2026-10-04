import { test, expect, openDashboard, actionResponse, configOf } from '../support/fixtures.js';

test('dashboard renders real empty-state, KPI, tools and navigates settings', async ({ page, request }) => {
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await openDashboard(page);
  const kpi = await (await request.get('/api/kpi')).json();
  await expect(page.locator('#kpiActive')).toHaveText(String(kpi.active_cases));
  await expect(page.locator('#caseRows')).toContainText('Belum ada kasus tercatat');
  await expect(page.locator('#caseCount')).toHaveText('0 kasus');
  await expect(page.locator('#bpStat')).not.toContainText('undefined');
  await page.locator('#toggleDiagnosis').click();
  await page.locator('.trace-disclosure summary').click();
  const tools = await (await request.get('/api/tools')).json();
  await expect(page.locator('#toolBtns button')).toHaveCount(tools.length);
  await page.locator('header').getByRole('link', { name: 'Pengaturan', exact: true }).click();
  await expect(page).toHaveURL(/settings.html$/);
  expect(errors).toEqual([]);
});

test('@audit API-02 diagnosis consumes actual Go SSE report and refreshes history', async ({ page, request }) => {
  await openDashboard(page);
  await page.locator('#toggleDiagnosis').click();
  await expect(page.locator('#mdl')).toHaveAttribute('readonly', '');
  await page.locator('#run').click();
  await expect(page.locator('#q')).toBeFocused();
  await page.locator('#q').fill('Halo, selamat pagi');
  const pending = page.waitForResponse(r => new URL(r.url()).pathname === '/api/ask');
  await page.locator('#run').click();
  const response = await pending;
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toContain('text/event-stream');
  expect(response.request().postDataJSON()).toEqual({ query: 'Halo, selamat pagi', target: '', stream: true });
  const events = (await response.text()).trim().split('\n\n').map(frame => {
    const [event, data] = frame.split('\n');
    return { event: event.slice(7), data: JSON.parse(data.slice(6)) };
  });
  const streamed = events.find(e => e.event === 'report').data;
  expect(events.filter(e => e.event === 'report')).toHaveLength(1);
  expect(events.some(e => e.event === 'error')).toBe(false);
  expect(streamed).toMatchObject({ id: expect.any(String), answer: expect.any(String), elapsed_ms: expect.any(Number) });
  // Chromium's protocol text view can decode charset-less SSE as Latin-1.
  // The persisted JSON report is the authoritative UTF-8 browser-render oracle.
  const reportsResponse = await request.get('/api/reports');
  expect(reportsResponse.status()).toBe(200);
  const reports = await reportsResponse.json();
  const report = reports.find(r => r.id === streamed.id);
  expect(report).toMatchObject({ id: streamed.id, engine: streamed.engine, elapsed_ms: streamed.elapsed_ms });
  expect(report.answer).toBe('UI regression response — café <b>literal</b>');
  await expect(page.locator('#answer')).toHaveText(report.answer);
  await expect(page.locator('#answer b')).toHaveCount(0);
  await expect(page.locator('#run')).toBeEnabled();
  await expect(page.locator('#hist')).toContainText(report.engine);
  expect(reports[0].id).toBe(report.id);
  await page.getByRole('button', { name: 'Bersihkan', exact: true }).click();
  await expect(page.locator('#steps li')).toHaveCount(0);
});

test('manual probe displays real allowlist rejection without network probe', async ({ page }) => {
  await openDashboard(page);
  await page.locator('#toggleDiagnosis').click();
  await page.locator('.trace-disclosure summary').click();
  await page.locator('#toolBtns').getByRole('button', { name: 'ping', exact: true }).click();
  await expect(page.locator('#probeTarget')).toBeFocused();
  await page.locator('#probeTarget').fill('192.0.2.42');
  const { response, sent, body } = await actionResponse(page, '/api/diag', 'POST', () => page.locator('#probeRun').click());
  expect(response.status()).toBe(403);
  expect(sent).toEqual({ tool: 'ping', target: '192.0.2.42' });
  await expect(page.locator('#steps')).toContainText(body.error);
});

test('cache confirmation supports cancel and real clear', async ({ page, request }) => {
  await openDashboard(page);
  const before = (await (await request.get('/api/sesi')).json()).statistik;
  page.once('dialog', d => d.dismiss());
  await page.getByRole('button', { name: /Kosongkan Cache/ }).click();
  expect((await (await request.get('/api/sesi')).json()).statistik.cache_entri).toBe(before.cache_entri);
  page.once('dialog', d => d.accept());
  const { response } = await actionResponse(page, '/api/sesi/cache', 'POST', () => page.getByRole('button', { name: /Kosongkan Cache/ }).click());
  expect(response.status()).toBe(200);
  expect((await (await request.get('/api/sesi')).json()).statistik.cache_entri).toBe(0);
});

test('WhatsApp blocklist normalizes, deduplicates, persists and removes', async ({ page, request, app }) => {
  await openDashboard(page);
  await page.locator('#adminDrawer summary').click();
  await page.getByRole('button', { name: /Kelola Daftar Blokir/ }).click();
  await expect(page.locator('#blModal [role=dialog]')).toBeVisible();
  await page.locator('#blInput').fill('+62 812-000-111');
  const add = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Tambah Nomor' }).click());
  expect(add.sent.wa_blocklist).toEqual(['62812000111']);
  expect(add.body.config.wa_blocklist).toEqual(['62812000111']);
  await page.locator('#blInput').fill('62812000111');
  await page.getByRole('button', { name: 'Tambah Nomor' }).click();
  await expect(page.locator('#blList li')).toHaveCount(1);
  expect((await app.config()).wa_blocklist).toEqual(['62812000111']);
  await actionResponse(page, '/api/config', 'POST', () => page.locator('#blList').getByRole('button', { name: 'Hapus' }).click());
  expect((await configOf(request)).wa_blocklist || []).toEqual([]);
  await page.getByRole('button', { name: 'Tutup Dialog' }).click();
  await expect(page.locator('#blModal [role=dialog]')).toBeHidden();
  await page.locator('#sWAGroup').selectOption('true');
  await page.locator('#sWAAsync').selectOption('true');
  const save = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Pengaturan', exact: true }).click());
  expect(save.body.config).toMatchObject({ wa_group: true, wa_async: true });
  await page.reload();
  await expect(page.locator('#sWAGroup')).toHaveValue('true');
  await expect(page.locator('#sWAAsync')).toHaveValue('true');
});
