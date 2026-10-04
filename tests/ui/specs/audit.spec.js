import { test, expect, login, keys, openSettings } from '../support/fixtures.js';

// Former integration findings are ordinary release-blocking regressions.
test('@audit AUTH-01 logout revokes only the copied session token server-side', async ({ request, playwright, app }) => {
  expect((await request.post('/api/auth/login', { data: login })).status()).toBe(200);
  const token = (await request.storageState()).cookies.find(c => c.name === 'noc_session').value;
  expect((await request.get('/api/auth/session')).status()).toBe(200);
  const other = await playwright.request.newContext({ baseURL: app.url });
  try {
    expect((await other.post('/api/auth/login', { data: login })).status()).toBe(200);
    expect((await request.post('/api/auth/logout')).status()).toBe(200);
    expect((await request.get('/api/auth/session')).status()).toBe(401);
    const replay = await request.get('/api/auth/session', { headers: { Cookie: `noc_session=${token}` } });
    expect(replay.status()).toBe(401);
    expect((await other.get('/api/auth/session')).status()).toBe(200);
    expect((await request.post('/api/auth/logout')).status()).toBe(200);
  } finally {
    await other.dispose();
  }
});

test('@audit API-01 ping B without explicit key uses saved B credentials', async ({ request, app }) => {
  for (const endpoint of ['b', 'reasoning', 'codex']) {
    const r = await request.post('/api/llm/ping', { data: { endpoint } });
    expect(r.status()).toBe(200);
    expect(await r.json()).toMatchObject({ ok: true, model: 'ui-model-b' });
    expect(app.requests.at(-1)).toMatchObject({ path: '/b/v1/chat/completions', authorization: `Bearer ${keys.b}`, body: { model: 'ui-model-b' } });
  }
  const query = await request.post('/api/llm/ping?endpoint=b', { data: {} });
  expect(query.status()).toBe(200);
  expect(app.requests.at(-1)).toMatchObject({ path: '/b/v1/chat/completions', authorization: `Bearer ${keys.b}` });
  const a = await request.post('/api/llm/ping', { data: {} });
  expect(a.status()).toBe(200);
  expect(app.requests.at(-1)).toMatchObject({ path: '/a/v1/chat/completions', authorization: `Bearer ${keys.a}` });
});

test('endpoint B URL overrides never leak saved credentials; explicit keys are not saved', async ({ request, app }) => {
  const before = await app.config();
  const base_url = `${app.provider}/b/unsaved/v1`;
  const missing = await request.post('/api/llm/ping', { data: { endpoint: 'b', base_url } });
  expect(missing.status()).toBe(502);
  expect(app.requests.at(-1).path).toBe('/b/unsaved/v1/chat/completions');
  expect(app.requests.at(-1).authorization || '').not.toContain(keys.a);
  expect(app.requests.at(-1).authorization || '').not.toContain(keys.b);
  const explicit = await request.post('/api/llm/ping', { data: { endpoint: 'b', base_url, api_key: keys.b } });
  expect(explicit.status()).toBe(200);
  expect(app.requests.at(-1).authorization).toBe(`Bearer ${keys.b}`);
  expect(await app.config()).toEqual(before);
});

test('@audit UI-01 settings includes shared shell, active navigation and working dashboard links', async ({ page }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const [name, hash, target] of [
    ['AI Diagnostics', '#diagnosisPanel', '#diagnosisPanel'],
    ['WhatsApp', '#adminDrawer', '#adminDrawer'],
    ['Kasus operasi', '#caseQueueTitle', '#caseQueueTitle'],
    ['Riwayat & sesi', '#historySection', '#historySection'],
    ['Overview', '', '#caseQueueTitle'],
  ]) {
    await openSettings(page);
    await expect(page.locator('script[src="/noc-shell.js"]')).toHaveCount(1);
    await expect(page.locator('.app-sidebar')).toHaveCount(1);
    await expect(page.locator('.app-sidebar')).toBeVisible();
    await expect(page.locator('.workspace-nav [aria-current="page"]')).toHaveText('Pengaturan');
    await page.locator('.workspace-nav').getByRole('link', { name, exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/${hash}$`));
    await expect(page.locator(target)).toBeVisible();
    if (hash === '#diagnosisPanel') await expect(page.locator('#toggleDiagnosis')).toHaveAttribute('aria-expanded', 'true');
    if (hash === '#adminDrawer') await expect(page.locator('#adminDrawer')).toHaveAttribute('open', '');
    await page.locator('.workspace-nav').getByRole('link', { name: 'Pengaturan', exact: true }).click();
    await expect(page).toHaveURL(/\/settings.html$/);
    await expect(page.locator('#pageMsg')).toContainText('Konfigurasi dimuat');
  }
  expect(errors).toEqual([]);
});
