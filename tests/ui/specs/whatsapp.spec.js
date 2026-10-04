import { test, expect, openDashboard, actionResponse } from '../support/fixtures.js';
import { qrPayload } from '../support/schemas.js';

for (const width of [1440, 768, 390]) {
  test(`long gateway logs scroll without overlapping WhatsApp controls at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 960 });
    const lines = Array.from({ length: 120 }, (_, i) => `[synthetic log ${i}] ${'long-token-'.repeat(50)}`);
    await page.route('**/api/wa/gateway', route => route.fulfill({ json: {
      state: 'running', pid: 123, port: 3001, restarts: 0, installed: true,
      credentials_linked: true, log_tail: lines
    } }));
    await openDashboard(page, '#adminDrawer');
    const log = page.locator('#waGwLog');
    await expect(log).toContainText('[synthetic log 119]');
    const geometry = await log.evaluate(el => ({
      bottom: el.getBoundingClientRect().bottom,
      nextTop: el.nextElementSibling.getBoundingClientRect().top,
      client: el.clientHeight, scroll: el.scrollHeight,
      overflow: getComputedStyle(el).overflowY,
      pageWidth: document.documentElement.scrollWidth, viewport: innerWidth
    }));
    expect(geometry.overflow).toBe('auto');
    expect(geometry.scroll).toBeGreaterThan(geometry.client);
    expect(geometry.client).toBeLessThanOrEqual(240);
    expect(geometry.nextTop).toBeGreaterThanOrEqual(geometry.bottom);
    expect(geometry.pageWidth).toBeLessThanOrEqual(geometry.viewport);
    await log.evaluate(el => { el.scrollTop = 0; });
    expect(await log.evaluate(el => el.scrollTop)).toBe(0);
    await page.getByRole('button', { name: /Kelola Daftar Blokir/ }).click();
    await expect(page.locator('#blModal')).toBeVisible();
    await page.getByRole('button', { name: 'Tutup Dialog' }).click();
    await page.getByRole('button', { name: 'Status Sesi', exact: true }).click();
    await expect(page.locator('#waMsgOut')).not.toBeEmpty();
  });
}


test('send requires recipient, explicit text and confirmation; real disabled gateway error', async ({ page }) => {
  await openDashboard(page, '#adminDrawer');
  await expect(page.locator('#adminDrawer')).toHaveAttribute('open', '');
  const send = page.getByRole('button', { name: 'Kirim Uji Coba' });
  const sent = [];
  page.on('request', r => { if (new URL(r.url()).pathname === '/api/wa/send') sent.push(r); });
  await send.click();
  await expect(page.locator('#waTo')).toBeFocused();
  await page.locator('#waTo').fill('628120001234');
  await send.click();
  await expect(page.locator('#waMsg')).toBeFocused();
  await expect(page.locator('#waMsgOut')).toContainText('Isi pesan uji coba');
  await page.locator('#waMsg').fill('Synthetic UI test message');
  page.once('dialog', d => d.dismiss());
  await send.click();
  expect(sent).toHaveLength(0);
  page.once('dialog', async d => {
    expect(d.message()).toContain('628120001234');
    await d.accept();
  });
  const result = await actionResponse(page, '/api/wa/send', 'POST', () => send.click());
  expect(result.sent).toEqual({ to: '628120001234', message: 'Synthetic UI test message' });
  expect(result.response.status()).toBeGreaterThanOrEqual(400);
  await expect(page.locator('#waMsgOut')).toContainText('Gagal');
});

test('gateway controls call Go but cannot start an external process in fixture', async ({ page }) => {
  await openDashboard(page, '#adminDrawer');
  await expect(page.locator('#adminDrawer')).toHaveAttribute('open', '');
  for (const [label, endpoint, method] of [
    ['Cek Proses', 'gateway', 'GET'], ['Status Sesi', 'status', 'GET'],
    ['▶ Mulai', 'start', 'POST'], ['■ Hentikan', 'stop', 'POST'], ['↻ Restart', 'restart', 'POST'],
    ['↻ Reconnect', 'reconnect', 'POST'],
  ]) {
    if (['stop', 'restart'].includes(endpoint)) page.once('dialog', d => d.accept());
    const { body } = await actionResponse(page, `/api/wa/${endpoint}`, method, () => page.getByRole('button', { name: label, exact: true }).click());
    expect(body).toEqual(expect.any(Object));
    await expect(page.locator('#btnWaStart')).toBeEnabled();
  }
  page.once('dialog', d => d.dismiss());
  await page.getByRole('button', { name: 'Logout Sesi', exact: true }).click();
  page.once('dialog', d => d.accept());
  await actionResponse(page, '/api/wa/logout', 'POST', () => page.getByRole('button', { name: 'Logout Sesi', exact: true }).click());
});

test('QR rendering accepts PNG data only, handles expiry and never leaks externally', async ({ page, app }) => {
  // Synthetic gateway response; real /api/wa/qr disabled contract is checked first.
  await openDashboard(page, '#adminDrawer');
  await expect(page.locator('#adminDrawer')).toHaveAttribute('open', '');
  const qr = page.getByRole('button', { name: 'QR Pairing' });
  const disabled = await actionResponse(page, '/api/wa/qr', 'GET', () => qr.click());
  expect(disabled.response.status()).toBeGreaterThanOrEqual(400);
  await expect(page.locator('#waQRBox')).toContainText('Gagal');
  const external = [];
  page.on('request', r => { if (/^https?:/.test(r.url()) && new URL(r.url()).origin !== app.url) external.push(r.url()); });
  await page.route('**/api/wa/qr', route => route.fulfill({ json: qrPayload }));
  await qr.click();
  await expect(page.locator('#waQRBox img')).toHaveAttribute('src', qrPayload.qr);
  await page.route('**/api/wa/qr', route => route.fulfill({ json: { ...qrPayload, is_expired: true } }));
  await qr.click();
  await expect(page.locator('#waQRBox')).toContainText('kedaluwarsa');
  await expect(page.locator('#waQRBox img')).toHaveCount(0);
  await page.route('**/api/wa/qr', route => route.fulfill({ json: { ...qrPayload, qr: 'private-pairing-material' } }));
  await qr.click();
  await expect(page.locator('#waQRBox')).toContainText('Format QR tidak didukung');
  await expect(page.locator('#waQRBox img')).toHaveCount(0);
  expect(external).toEqual([]);
});
