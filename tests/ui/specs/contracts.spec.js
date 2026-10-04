import { test, expect, login, configOf, keys } from '../support/fixtures.js';

test('auth HTTP contracts: methods, invalid JSON, login session logout', async ({ request }) => {
  expect((await request.get('/api/auth/session')).status()).toBe(401);
  for (const endpoint of ['login', 'logout']) {
    const r = await request.get(`/api/auth/${endpoint}`);
    expect(r.status()).toBe(405);
    expect(await r.json()).toMatchObject({ ok: false, error: expect.any(String) });
  }
  expect((await request.post('/api/auth/login', { data: '{', headers: { 'Content-Type': 'application/json' } })).status()).toBe(400);
  expect((await request.post('/api/auth/login', { data: { ...login, password: 'wrong' } })).status()).toBe(401);
  const response = await request.post('/api/auth/login', { data: login });
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual({ ok: true, username: login.username });
  expect(response.headers()['set-cookie']).toContain('HttpOnly');
  expect(await (await request.get('/api/auth/session')).json()).toEqual({ ok: true, authenticated: true });
  expect(await (await request.post('/api/auth/logout')).json()).toEqual({ ok: true });
  expect((await request.get('/api/auth/session')).status()).toBe(401);
});

test('real dashboard/settings read contracts and redaction', async ({ request, app }) => {
  const config = await configOf(request);
  expect(config).toMatchObject({ llm_model: 'ui-model-a', max_steps: 2, llm_key_set: true, codex_key_set: true });
  const serialized = JSON.stringify(config);
  for (const secret of [login.password, keys.a, keys.b, 'disposable-ui-test-salt']) expect(serialized).not.toContain(secret);
  expect(config.auth_users).toEqual([login.username]); // Public usernames, never password hashes.
  const reads = {
    '/api/tools': value => expect(value).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'ping', description: expect.any(String) })])),
    '/api/reports': value => expect(Array.isArray(value)).toBe(true),
    '/api/cases': value => expect(value).toMatchObject({ count: 0, cases: [] }),
    '/api/alerts': value => expect(Array.isArray(value)).toBe(true),
    '/api/blueprint': value => expect(value).toMatchObject({ mode: expect.any(String), tools: { aktif: expect.any(Number), total: expect.any(Number) }, workflow: expect.any(Array) }),
    '/api/sesi': value => expect(value).toMatchObject({ statistik: { cache_entri: expect.any(Number), percakapan_aktif: expect.any(Number) } }),
    '/api/staff': value => expect(value).toMatchObject({ ok: true, roles: expect.any(Array), role_perms: expect.any(Object) }),
    '/api/rules': value => expect(value).toMatchObject({ mode: expect.any(String), rules: expect.any(Array) }),
    '/api/recipes': value => expect(value.resep).toEqual([app.recipe]),
    '/api/team/metrics': value => expect(value).toMatchObject({ flags: { routing: 'off', cs_scope: 'off', handoff: 'off', severity: 'off', presenter: 'off', noc_toolfirst: 'off' } }),
  };
  for (const [url, check] of Object.entries(reads)) {
    await test.step(url, async () => {
      const response = await request.get(url);
      expect(response.status()).toBe(200);
      expect(response.headers()['content-type']).toContain('application/json');
      check(await response.json());
    });
  }
  const kpi = await (await request.get('/api/kpi')).json();
  for (const key of ['active_cases', 'escalated', 'verified_resolved', 'escalation_rate_percent', 'total_cases']) {
    expect(kpi[key]).toBe(0);
  }
});

test('models endpoint A/B selection uses separate persisted provider keys', async ({ request, app }) => {
  for (const endpoint of ['a', 'b']) {
    const r = await request.post(`/api/models?endpoint=${endpoint}`, { data: {} });
    expect(r.status()).toBe(200);
    expect(await r.json()).toEqual({ count: 2, endpoint, models: ['ui-model-a', 'ui-model-b'] });
    expect(app.requests.some(r => r.path === `/${endpoint}/v1/models` && r.authorization === `Bearer ${keys[endpoint]}`)).toBe(true);
  }
});

test('write contracts reject invalid and unauthorized operations', async ({ request }) => {
  expect((await request.get('/api/config')).status()).toBe(405);
  expect((await request.post('/api/config', { data: '{', headers: { 'Content-Type': 'application/json' } })).status()).toBe(400);
  expect((await request.post('/api/ask', { data: { query: '  ', stream: true } })).status()).toBe(400);
  for (const [url, method, data] of [
    ['/api/rules', 'post', { number: '628120000000', pin: 'invalid', rules: [] }],
    ['/api/recipes', 'delete', { number: '628120000000', pin: 'invalid', signature: 'ui-recipe', tools: 'ping' }],
  ]) {
    const r = await request[method](url, { data });
    expect(r.status()).toBe(403);
    expect(await r.json()).toMatchObject({ ok: false, error: expect.any(String) });
  }
});

test('HTML and shared assets are served by actual embedded backend', async ({ request }) => {
  for (const url of ['/', '/settings.html', '/login.html']) {
    const r = await request.get(url);
    expect(r.status()).toBe(200);
    expect(r.headers()['content-type']).toContain('text/html');
    expect(r.headers()['cache-control']).toContain('no-store');
  }
  for (const [url, type] of [['/noc-shell.js', 'javascript'], ['/noc-ui.css', 'text/css'], ['/logo.png', 'image/png']]) {
    const r = await request.get(url);
    expect(r.status()).toBe(200);
    expect(r.headers()['content-type']).toContain(type);
  }
});
