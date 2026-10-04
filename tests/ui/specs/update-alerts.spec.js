import { test, expect, openDashboard, actionResponse } from '../support/fixtures.js';

test('alert disclosure escapes authentic observability Alert fields', async ({ page }) => {
  // internal/observability/observability.go: Alert; synthetic unavailable dependency.
  await page.route('**/api/alerts', route => route.fulfill({ json: [{ severity: 'critical', source: 'dependency', subject: 'UI <b>router</b>', message: 'Synthetic <img> failure' }] }));
  await openDashboard(page);
  await page.locator('#alertBanner summary').click();
  await expect(page.locator('#alertBanner li')).toContainText('UI <b>router</b>');
  await expect(page.locator('#alertBanner li')).toContainText('Synthetic <img> failure');
  await expect(page.locator('#alertBanner img, #alertBanner b')).toHaveCount(0);
});

test('update confirmation cancels then reports real disabled-updater contract', async ({ page, request }) => {
  expect((await (await request.get('/api/update/check')).json()).ok).toBe(false);
  // internal/updater/updater.go: Status/Release. Only availability is synthesized.
  await page.route('**/api/update/check', route => route.fulfill({ json: {
    current: { version: 'dev' }, update_available: true, checked_at: '2026-01-01T00:00:00Z',
    latest: { tag: 'v99.0.0', version: '99.0.0', name: 'UI fixture', notes: 'Synthetic release',
      url: '', published_at: '2026-01-01T00:00:00Z', prerelease: false },
  } }));
  await openDashboard(page);
  await expect(page.locator('#btnUpdate')).toBeVisible();
  const requests = [];
  page.on('request', r => { if (new URL(r.url()).pathname === '/api/update/apply') requests.push(r); });
  page.once('dialog', d => d.dismiss());
  await page.locator('#btnUpdate').click();
  expect(requests).toHaveLength(0);
  const dialogs = [];
  page.on('dialog', async d => { dialogs.push({ type: d.type(), message: d.message() }); await d.accept(); });
  const applied = await actionResponse(page, '/api/update/apply', 'POST', () => page.locator('#btnUpdate').click());
  expect(applied.body).toEqual({ ok: false, error: 'fitur update tidak aktif' });
  await expect(page.locator('#btnUpdate')).toBeEnabled();
  expect(dialogs).toEqual([
    { type: 'confirm', message: expect.stringContaining('RESTART') },
    { type: 'alert', message: expect.stringContaining('fitur update tidak aktif') },
  ]);
});
