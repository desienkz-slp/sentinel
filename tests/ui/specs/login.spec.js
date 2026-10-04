import { test, expect, login, actionResponse, openDashboard } from '../support/fixtures.js';

test('login validates required fields and recovers after real 401', async ({ page }) => {
  await page.goto('/login.html');
  await page.locator('#submitBtn').click();
  await expect(page.locator('#u')).toBeFocused();
  await page.locator('#u').fill(login.username);
  await page.locator('#p').fill('incorrect-synthetic-password');
  const { response, body, sent } = await actionResponse(page, '/api/auth/login', 'POST', () => page.locator('#submitBtn').click());
  expect(response.status()).toBe(401);
  expect(response.headers()['content-type']).toContain('application/json');
  expect(sent.username).toBe(login.username);
  await expect(page.locator('#err')).toBeVisible();
  await expect(page.locator('#submitBtn')).toBeEnabled();
  await expect(page).toHaveURL(/login.html$/);
});

for (const destination of ['/', '/settings.html']) {
  test(`real login cookie, session and logout from ${destination}`, async ({ page, context }) => {
    await page.goto('/login.html');
    await page.locator('#u').fill(`  ${login.username}  `);
    await page.locator('#p').fill(login.password);
    const { response, body, sent } = await actionResponse(page, '/api/auth/login', 'POST', () => page.locator('#submitBtn').click());
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toContain('application/json');
    expect(sent).toEqual(login);
    await expect(page).toHaveURL(/\/$/);
    const cookie = (await context.cookies()).find(c => c.name === 'noc_session');
    expect(cookie).toMatchObject({ httpOnly: true, sameSite: 'Strict', path: '/' });
    expect(await (await context.request.get('/api/auth/session')).json()).toEqual({ ok: true, authenticated: true });
    await page.goto(destination);
    const logout = await actionResponse(page, '/api/auth/logout', 'POST', () => page.locator('#btnLogout').click());
    expect(logout.response.status()).toBe(200);
    await expect(page).toHaveURL(/login.html$/);
    expect((await context.cookies()).some(c => c.name === 'noc_session')).toBe(false);
    expect((await context.request.get('/api/auth/session')).status()).toBe(401);
    // Cookie removal alone is insufficient: the server must reject token replay.
    expect((await context.request.get('/api/auth/session', {
      headers: { Cookie: `noc_session=${cookie.value}` },
    })).status()).toBe(401);
  });
}

test('login network failure releases submit button', async ({ page }) => {
  await page.goto('/login.html');
  // Deliberate transport fault; normal success/error contracts above use Go.
  await page.route('**/api/auth/login', route => route.abort('connectionfailed'));
  await page.locator('#u').fill(login.username);
  await page.locator('#p').fill(login.password);
  await page.locator('#submitBtn').click();
  await expect(page.locator('#err')).toBeVisible();
  await expect(page.locator('#submitBtn')).toBeEnabled();
});
