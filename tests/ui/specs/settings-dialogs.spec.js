import { test, expect, openSettings, navigateSettings, actionResponse, configOf, keys } from '../support/fixtures.js';

const admin = { number: '628120000555', pin: 'ui-test-pin' };
async function createAdmin(request) {
  const r = await request.post('/api/staff', { data: { ...admin, name: 'Synthetic admin', role: 'super_admin', active: true } });
  expect(r.status()).toBe(200);
}
async function fillAdmin(page, pin = admin.pin) {
  await expect(page.locator('#adminDialog')).toBeVisible();
  await page.locator('#adminNumber').fill(admin.number);
  await page.locator('#adminPin').fill(pin);
}

test('@audit API-03 recipe delete confirms, cancels, rejects wrong PIN then persists deletion across restart', async ({ page, request, app }) => {
  await createAdmin(request);
  await openSettings(page, '#recipesCard');
  const remove = page.getByRole('button', { name: 'Hapus resep ui-recipe', exact: true });
  await expect(remove).toBeVisible();
  page.once('dialog', d => d.dismiss());
  await remove.click();
  await expect(page.locator('#adminDialog')).toBeHidden();
  page.once('dialog', d => d.accept());
  await remove.click();
  await page.locator('#adminCancel').click();
  await expect(remove).toBeVisible();
  page.once('dialog', d => d.accept());
  await remove.click();
  await fillAdmin(page, 'wrong-pin');
  const denied = await actionResponse(page, '/api/recipes', 'DELETE', () => page.getByRole('button', { name: 'Otorisasi & lanjutkan' }).click());
  expect(denied.response.status()).toBe(403);
  await expect(page.locator('#recipesMsg')).toContainText('PIN');
  await expect(page.locator('#adminPin')).toHaveValue('');
  expect((await (await request.get('/api/recipes')).json()).resep).toHaveLength(1);
  expect((await app.recipes()).recipes['ui-recipe']).toHaveLength(1);
  page.once('dialog', d => d.accept());
  await remove.click();
  await fillAdmin(page);
  const deleted = await actionResponse(page, '/api/recipes', 'DELETE', () => page.getByRole('button', { name: 'Otorisasi & lanjutkan' }).click());
  expect(deleted.sent).toEqual({ ...admin, signature: 'ui-recipe', tools: 'ping' });
  expect(deleted.response.status()).toBe(200);
  expect(deleted.body).toEqual({ ok: true });
  await expect(page.locator('#recipesMsg')).toHaveText('Resep dihapus.');
  await expect(page.locator('#recipesList')).toContainText('Belum ada resep');
  await expect(page.locator('#adminPin')).toHaveValue('');
  const remaining = await request.get('/api/recipes', { timeout: 5000 });
  expect(remaining.status()).toBe(200);
  expect((await remaining.json()).resep).toEqual([]);
  expect((await app.recipes()).recipes).not.toHaveProperty('ui-recipe');
  await app.restart();
  await openSettings(page, '#recipesCard');
  await expect(remove).toHaveCount(0);
  await expect(page.locator('#recipesList')).toContainText('Belum ada resep');
  expect((await (await request.get('/api/recipes', { timeout: 5000 })).json()).resep).toEqual([]);
  const again = await request.delete('/api/recipes', { data: { ...admin, signature: 'ui-recipe', tools: 'ping' }, timeout: 5000 });
  expect(again.status()).toBe(404);
});

for (const decision of ['REMOVE', '']) {
  test(`@audit POLICY-01 ${decision || 'empty decision'} survives repeated browser saves, reload and restart`, async ({ page, request, app }) => {
    await createAdmin(request);
    const baseline = await app.policyBaseline();
    await openSettings(page, '#policyCard');
    const before = (await (await request.get('/api/rules')).json()).rules;
    expect(before.length).toBeGreaterThan(1);
    const [removed, edited] = before;
    const choice = page.getByRole('combobox', { name: `Keputusan: ${removed.id}`, exact: true });
    const permission = page.getByRole('textbox', { name: `Permission: ${edited.id}`, exact: true });
    const save = async (pin = admin.pin) => {
      await page.getByRole('button', { name: 'Simpan Perubahan Aturan' }).click();
      await fillAdmin(page, pin);
      const result = await actionResponse(page, '/api/rules', 'POST', () => page.getByRole('button', { name: 'Otorisasi & lanjutkan' }).click());
      await expect(page.locator('#adminPin')).toHaveValue('');
      await expect(page.getByRole('button', { name: 'Simpan Perubahan Aturan' })).toBeEnabled();
      return result;
    };
    const assertPersisted = async (expectedPermission) => {
      const r = await request.get('/api/rules');
      expect(r.status()).toBe(200);
      const rules = (await r.json()).rules;
      expect(rules.some(rule => rule.id === removed.id)).toBe(false);
      expect(rules.find(rule => rule.id === edited.id).permission).toBe(expectedPermission);
      expect(rules).toHaveLength(before.length - 1);
      expect(await app.policyBaseline()).toBe(baseline);
      // The baseline rule must retain an on-disk deletion marker, not just disappear in memory.
      expect(await app.policyOverlay()).toContain(`id: ${removed.id}`);
    };
    await choice.selectOption(decision);
    const denied = await save('wrong-pin');
    expect(denied.response.status()).toBe(403);
    await expect(page.locator('#rulesMsg')).toContainText('PIN');
    expect((await (await request.get('/api/rules')).json()).rules).toEqual(before);
    const first = await save();
    expect(first.response.status()).toBe(200);
    expect(first.sent.rules.find(rule => rule.id === removed.id).decision).toBe(decision);
    await expect(page.locator('#rulesMsg')).toContainText('tersimpan');
    await assertPersisted(edited.permission);
    // Saving another edit in the same document must retain the deletion marker.
    await permission.fill('ui-regression-first');
    expect((await save()).response.status()).toBe(200);
    await assertPersisted('ui-regression-first');
    // Start a fresh document: in-memory tombstone caches cannot satisfy this regression.
    await page.reload();
    await expect(page.locator('#pageMsg')).toContainText('Konfigurasi dimuat');
    await expect(permission).toHaveValue('ui-regression-first');
    await permission.fill('ui-regression-second');
    expect((await save()).response.status()).toBe(200);
    await assertPersisted('ui-regression-second');
    await app.restart();
    await openSettings(page, '#policyCard');
    await expect(permission).toHaveValue('ui-regression-second');
    await assertPersisted('ui-regression-second');
  });
}

test('clearing endpoint and integration URLs sends explicit empty strings', async ({ page, request, app }) => {
  await request.post('/api/config', { data: { billing_url: app.provider, radius_url: app.provider, genieacs_url: app.provider } });
  await openSettings(page);
  await page.locator('#sCodexBase').fill('');
  const ai = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Endpoint & Perilaku Agen' }).click());
  expect(ai.sent.codex_base_url).toBe('');
  expect(ai.body.config.codex_base_url).toBe('');
  await navigateSettings(page, '#integrations');
  for (const id of ['billingUrl', 'radiusUrl', 'genieacsUrl']) await page.locator(`#${id}`).fill('');
  const integrations = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Integrasi', exact: true }).click());
  expect(integrations.sent).toMatchObject({ billing_url: '', radius_url: '', genieacs_url: '' });
  expect(await configOf(request)).toMatchObject({ billing_url: '', radius_url: '', genieacs_url: '' });
});

test('Endpoint B browser ping uses saved B key, honors explicit override and does not persist', async ({ page, app }) => {
  const before = await app.config();
  await openSettings(page);
  const ping = page.getByRole('button', { name: 'Tes Koneksi Real', exact: true }).nth(1);
  await expect(page.locator('#sCodexKey')).toHaveValue('');
  const savedKey = await actionResponse(page, '/api/llm/ping', 'POST', () => ping.click());
  expect(savedKey.response.status()).toBe(200);
  expect(savedKey.sent).toMatchObject({ endpoint: 'b', base_url: `${app.provider}/b/v1`, model: 'ui-model-b' });
  expect(savedKey.sent).not.toHaveProperty('api_key');
  expect(app.requests.at(-1)).toMatchObject({ path: '/b/v1/chat/completions', authorization: `Bearer ${keys.b}` });
  await expect(page.locator('#setMsgB')).toContainText('Endpoint B menjawab');
  // Deliberately use A's distinct key to prove an explicit key overrides saved B.
  await page.locator('#sCodexKey').fill(keys.a);
  const rejected = await actionResponse(page, '/api/llm/ping', 'POST', () => ping.click());
  expect(rejected.response.status()).toBe(502);
  expect(app.requests.at(-1).authorization).toBe(`Bearer ${keys.a}`);
  await expect(page.locator('#setMsgB')).toContainText('Endpoint B gagal');
  await page.locator('#sCodexKey').fill(keys.b);
  const checked = await actionResponse(page, '/api/llm/ping', 'POST', () => ping.click());
  expect(checked.response.status()).toBe(200);
  expect(checked.sent).toMatchObject({ api_key: keys.b, base_url: `${app.provider}/b/v1`, model: 'ui-model-b' });
  await expect(page.locator('#setMsgB')).toContainText('Endpoint B menjawab');
  expect(app.requests.at(-1)).toMatchObject({ path: '/b/v1/chat/completions', authorization: `Bearer ${keys.b}` });
  expect(await app.config()).toEqual(before);
});

test('boot failure disables writes, retry recovers and invalid settings stay local', async ({ page }) => {
  await page.route('**/api/status', route => route.fulfill({ status: 503, json: { error: 'fixture offline' } }));
  await page.goto('/settings.html#integrations');
  await expect(page.locator('#retryBoot')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Simpan Integrasi', exact: true })).toBeDisabled();
  await page.unroute('**/api/status');
  await page.locator('#retryBoot').click();
  await expect(page.locator('#pageMsg')).toContainText('Konfigurasi dimuat');
  await navigateSettings(page, '#agent');
  await page.locator('#sSteps').fill('21');
  await page.getByRole('button', { name: 'Simpan Endpoint & Perilaku Agen' }).click();
  await expect(page.locator('#setMsg')).toContainText('1–20');
  await page.locator('#sSteps').fill('2');
  await navigateSettings(page, '#conversation');
  await page.locator('#sBase').fill('javascript:invalid');
  await page.getByRole('button', { name: 'Simpan Endpoint & Perilaku Agen' }).click();
  await expect(page.locator('#setMsg')).toContainText('Periksa URL');
});
