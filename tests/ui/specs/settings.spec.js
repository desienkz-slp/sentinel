import { test, expect, openSettings, navigateSettings, actionResponse, configOf, keys } from '../support/fixtures.js';

test('settings save round-trips fields and preserves omitted secrets', async ({ page, request, app }) => {
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await openSettings(page);
  await expect(page.locator('#sKey')).toHaveValue('');
  await page.getByRole('button', { name: 'Cek Daftar Model', exact: true }).first().click();
  await expect(page.locator('#setMsgA')).toContainText('model dimuat');
  await page.locator('#sModel').selectOption('ui-model-b');
  await navigateSettings(page, '#agent');
  await page.locator('#sSteps').fill('7');
  await page.locator('#greetingFirstMessage').check();
  await page.locator('#greetingTemplate').fill('Halo {{name}} — UI regression');
  const { response, sent, body } = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Endpoint & Perilaku Agen' }).click());
  expect(response.status()).toBe(200);
  expect(sent).toMatchObject({ max_steps: 7, llm_model: 'ui-model-b', greeting_first_message: true });
  expect(sent).not.toHaveProperty('llm_api_key');
  expect(sent).not.toHaveProperty('codex_api_key');
  expect(body.config).toMatchObject({ max_steps: 7, llm_model: 'ui-model-b', llm_key_set: true, codex_key_set: true });
  expect((await app.config()).llm_api_key).toBe(keys.a);
  await expect(page.locator('#setMsg')).toContainText('Tersimpan');
  await page.reload();
  await expect(page.locator('#sSteps')).toHaveValue('7');
  await navigateSettings(page, '#conversation');
  await expect(page.locator('#sModel')).toHaveValue('ui-model-b');
  expect((await configOf(request)).greeting_template).toBe('Halo {{name}} — UI regression');
  expect(errors).toEqual([]);
});

test('conversation connection check reaches real API and local provider', async ({ page, app }) => {
  await openSettings(page);
  const { response, sent, body } = await actionResponse(page, '/api/llm/ping', 'POST', () => page.getByRole('button', { name: 'Tes Koneksi Real', exact: true }).first().click());
  expect(response.status()).toBe(200);
  expect(sent).toMatchObject({ base_url: `${app.provider}/a/v1`, model: 'ui-model-a', wire_api: 'chat' });
  expect(body).toMatchObject({ ok: true, model: 'ui-model-a', latency_ms: expect.any(Number) });
  await expect(page.locator('#setMsgA')).toContainText('LLM menjawab');
  expect(app.requests.some(r => r.path === '/a/v1/chat/completions' && r.authorization === `Bearer ${keys.a}`)).toBe(true);
});

test('all team mode fields persist through real metrics API', async ({ page, request }) => {
  await openSettings(page, '#teamCard');
  for (const id of ['tmRouting', 'tmCSScope', 'tmPresenter', 'tmHandoff', 'tmSeverity', 'tmNOCTools']) {
    await page.locator(`#${id}`).selectOption('shadow');
  }
  const { sent } = await actionResponse(page, '/api/config', 'POST', () => page.getByRole('button', { name: 'Simpan Mode Tim' }).click());
  expect(Object.values(sent)).toEqual(Array(6).fill('shadow'));
  const metrics = await (await request.get('/api/team/metrics')).json();
  expect(Object.values(metrics.flags)).toEqual(Array(6).fill('shadow'));
  await expect(page.locator('#tmMsg')).toContainText('tersimpan');
});

test('staff validates required fields and supports create edit delete', async ({ page, request }) => {
  await openSettings(page, '#staffCard');
  await page.getByRole('button', { name: 'Simpan Data Staf' }).click();
  await expect(page.locator('#staffMsg')).toContainText('wajib diisi');
  await page.locator('#stfNumber').fill('628120009999');
  await page.locator('#stfName').fill('UI <b>Operator</b>');
  await page.locator('#stfRole').selectOption('noc_senior');
  const saved = await actionResponse(page, '/api/staff', 'POST', () => page.getByRole('button', { name: 'Simpan Data Staf' }).click());
  expect(saved.response.status()).toBe(200);
  expect(saved.sent).toMatchObject({ role: 'noc_senior', active: true });
  const row = page.locator('#staffList tr').filter({ hasText: '628120009999' });
  await expect(row).toContainText('UI <b>Operator</b>');
  await expect(row.locator('b b')).toHaveCount(0);
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('#stfNumber')).toHaveValue('628120009999');
  await page.locator('#stfTitle').fill('Regression engineer');
  await actionResponse(page, '/api/staff', 'POST', () => page.getByRole('button', { name: 'Simpan Data Staf' }).click());
  await expect(row).toContainText('Regression engineer');
  page.once('dialog', d => d.dismiss());
  await row.getByRole('button', { name: 'Hapus', exact: true }).click();
  await expect(row).toBeVisible();
  page.once('dialog', d => d.accept());
  const deleted = await actionResponse(page, '/api/staff', 'DELETE', () => row.getByRole('button', { name: 'Hapus', exact: true }).click());
  expect(deleted.response.status()).toBe(200);
  await expect(row).toHaveCount(0);
  expect((await (await request.get('/api/staff')).json()).staff || []).toEqual([]);
});

test('integration checks show backend unconfigured errors', async ({ page }) => {
  await openSettings(page, '#integrations');
  for (const [label, route] of [['Billing', 'billing'], ['MikroTik', 'mikrotik'], ['RADIUS', 'radius'], ['GenieACS', 'genieacs']]) {
    const { body } = await actionResponse(page, `/api/${route}/check`, 'GET', () => page.getByRole('button', { name: label === 'MikroTik' ? 'Tes Router Utama' : `Tes Koneksi ${label}`, exact: true }).click());
    expect(body.ok).toBe(false);
    expect(body.error).toEqual(expect.any(String));
    await expect(page.locator('#intMsg')).toContainText(body.error);
  }
});

test('router add validates locally and rule reload discards unsaved removal', async ({ page, request }) => {
  await openSettings(page, '#integrations');
  await page.getByRole('button', { name: /Tambah Router MikroTik/ }).click();
  await page.getByRole('button', { name: 'Tes Koneksi Router', exact: true }).click();
  await expect(page.locator('#intMsg')).toContainText('Isi Host dan Username');
  page.once('dialog', d => d.accept());
  await page.getByRole('button', { name: 'Hapus Router', exact: true }).click();
  await expect(page.locator('#mkList .mk-row')).toHaveCount(0);
  const count = (await (await request.get('/api/rules')).json()).rules.length;
  expect(count).toBeGreaterThan(0);
  await navigateSettings(page, '#policyCard');
  await expect(page.locator('#rulesList .rule-row')).toHaveCount(count);
  page.once('dialog', d => d.accept());
  await page.locator('#rulesList button').first().click();
  await expect(page.locator('#rulesList select').first()).toHaveValue('REMOVE');
  page.once('dialog', d => d.accept());
  await page.getByRole('button', { name: /Muat Ulang Aturan/ }).click();
  await expect(page.locator('#rulesList .rule-row')).toHaveCount(count);
  await page.getByRole('button', { name: 'Simpan Perubahan Aturan' }).click();
  await expect(page.locator('#adminDialog')).toBeVisible();
  await page.locator('#adminCancel').click();
  await expect(page.locator('#adminDialog')).toBeHidden();
  expect((await (await request.get('/api/rules')).json()).rules).toHaveLength(count);
});
