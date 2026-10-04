import { test as base, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { createHmac } from 'node:crypto';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { root, runtime, binaryName } from './build.js';
import { trackerSeed } from './schemas.js';

// Synthetic credentials, only valid in the disposable test server.
export const login = { username: 'ui-operator', password: 'ui-only-not-a-real-password' };
export const keys = { a: 'ui-provider-a-only', b: 'ui-provider-b-only' };
const listen = async server => {
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
};

async function startApp(seedCases) {
  const dir = await mkdtemp(path.join(runtime, 'case-'));
  let child, upstream;
  const requests = [];
  let log = '';
  const stopProcess = async () => {
    if (child && child.exitCode === null) {
      const ended = once(child, 'exit');
      child.kill();
      await ended;
    }
  };
  const stop = async () => {
    await stopProcess();
    if (upstream) {
      upstream.closeAllConnections();
      await new Promise(resolve => upstream.close(resolve));
    }
    await rm(dir, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  };
  try {
    upstream = createServer(async (req, res) => {
      let text = '';
      for await (const chunk of req) text += chunk;
      const body = text ? JSON.parse(text) : undefined;
      requests.push({ path: req.url, method: req.method, authorization: req.headers.authorization, body });
      const json = (status, data) => {
        res.writeHead(status, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(data));
      };
      if (req.url.endsWith('/models')) {
        return json(200, { data: ['ui-model-a', 'ui-model-b'].map(id => ({ id })) });
      }
      if (req.url.endsWith('/chat/completions')) {
        const expected = req.url.startsWith('/b/') ? keys.b : keys.a;
        if (req.headers.authorization !== `Bearer ${expected}`) {
          return json(401, { error: { message: 'synthetic provider key mismatch' } });
        }
        return json(200, { choices: [{ message: { role: 'assistant', content: 'UI regression response — café <b>literal</b>' }, finish_reason: 'stop' }] });
      }
      // No WhatsApp provider or real integration exists in this fixture.
      return json(503, { error: 'external integration disabled in UI fixture' });
    });
    const provider = await listen(upstream);
    const reservation = createServer();
    const url = await listen(reservation);
    await new Promise(resolve => reservation.close(resolve));
    await mkdir(path.join(dir, 'data'));
    await copyFile(path.join(runtime, binaryName), path.join(dir, binaryName));
    for (const name of ['policies', 'workflows', 'tools']) {
      await mkdir(path.join(dir, name), { recursive: true });
    }
    await copyFile(path.join(root, 'policies/default-policy.yaml'), path.join(dir, 'policies/default-policy.yaml'));
    // Copy release-controlled files, not operator-local overlays.
    for (const name of ['registry.yaml', 'read_only_external_api_manifest.yaml']) {
      await copyFile(path.join(root, 'tools', name), path.join(dir, 'tools', name));
    }
    const recipe = { signature: 'ui-recipe', tools: ['ping'], verdict: 'SEHAT', hits: 2,
      first_seen: '2026-01-01T00:00:00Z', last_used: '2026-01-01T00:00:00Z' };
    await writeFile(path.join(dir, 'data/recipes.json'), JSON.stringify({ recipes: { 'ui-recipe': [recipe] } }));
    if (seedCases) await writeFile(path.join(dir, 'data/cases.json'), JSON.stringify(trackerSeed));
    const salt = 'disposable-ui-test-salt';
    const config = {
      addr: new URL(url).host,
      auth_salt: salt,
      auth_users: { [login.username]: createHmac('sha256', salt).update(login.password).digest('hex') },
      llm_base_url: `${provider}/a/v1`, llm_api_key: keys.a, llm_model: 'ui-model-a', llm_wire_api: 'chat',
      codex_base_url: `${provider}/b/v1`, codex_api_key: keys.b, codex_model: 'ui-model-b', codex_wire_api: 'chat',
      codex_path: path.join(dir, 'no-codex'), llm_timeout_sec: 3, diag_timeout_sec: 1, max_steps: 2,
      wa_base_url: '', wa_dir: '', wa_autostart: false, wa_auto_reply: false,
      wa_blocklist: [], staff_members: [], diag_allowlist: ['127.0.0.1/32'],
      billing_url: '', radius_url: '', genieacs_url: '', mikrotik_routers: [],
      update_owner: '', update_repo: '', store_pg: 'off', store_redis: 'off',
      team_routing: 'off', team_cs_scope: 'off', team_handoff: 'off', team_severity: 'off',
      team_presenter: 'off', team_noc_toolfirst: 'off',
      memory_path: path.join(dir, 'data/memory.json'), incident_path: path.join(dir, 'data/incidents.json'),
      audit_path: path.join(dir, 'data/audit.json'),
    };
    const configPath = path.join(dir, 'config.json');
    await writeFile(configPath, JSON.stringify(config));
    // Do not inherit production NOC endpoints, credentials, database settings or proxies.
    const env = Object.fromEntries(Object.entries(process.env).filter(([key]) =>
      !/^(NOC_|LLM_|OPENAI_|HERMES_CUSTOM_|CODEX_|HTTP_PROXY$|HTTPS_PROXY$|ALL_PROXY$)/i.test(key)));
    Object.assign(env, { NOC_POSTGRES_PORT: '1', NOC_REDIS_ADDR: '127.0.0.1:1', NO_PROXY: '*' });
    const startProcess = async () => {
      child = spawn(path.join(dir, binaryName), ['-config', configPath], { cwd: dir, env, windowsHide: true });
      child.stdout.on('data', chunk => { log += chunk; });
      child.stderr.on('data', chunk => { log += chunk; });
      let spawnError;
      child.on('error', error => { spawnError = error; });
      const deadline = Date.now() + 15_000;
      while (Date.now() < deadline) {
        if (spawnError) throw spawnError;
        if (child.exitCode !== null) throw new Error(`Go server exited: ${log}`);
        try {
          const response = await fetch(`${url}/healthz`);
          if (response.ok) return;
        } catch { /* retry until readiness or deadline */ }
        await new Promise(resolve => setTimeout(resolve, 50));
      }
      throw new Error(`Go server did not become ready: ${log}`);
    };
    await startProcess();
    return { url, provider, requests, recipe, configPath,
      config: async () => JSON.parse(await readFile(configPath, 'utf8')),
      recipes: async () => JSON.parse(await readFile(path.join(dir, 'data/recipes.json'), 'utf8')),
      policyBaseline: () => readFile(path.join(dir, 'policies/default-policy.yaml'), 'utf8'),
      policyOverlay: () => readFile(path.join(dir, 'policies/policy.local.yaml'), 'utf8'),
      restart: async () => { await stopProcess(); await startProcess(); },
      stop, logs: () => log };
  } catch (error) {
    await stop();
    throw error;
  }
}

export const test = base.extend({
  seedCases: [false, { option: true }],
  app: async ({ seedCases }, use, testInfo) => {
    const app = await startApp(seedCases);
    try { await use(app); }
    finally {
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach('go-server.log', { body: app.logs(), contentType: 'text/plain' });
        await testInfo.attach('provider-requests.json', { body: JSON.stringify(app.requests, null, 2), contentType: 'application/json' });
      }
      await app.stop();
    }
  },
  baseURL: async ({ app }, use) => use(app.url),
  page: async ({ page, app }, use) => {
    await page.route('**/*', route => {
      const url = new URL(route.request().url());
      return url.origin === app.url ? route.continue() : route.abort('blockedbyclient');
    });
    await use(page);
  },
});
export { expect };

export async function openSettings(page, hash = '') {
  await page.goto(`/settings.html${hash}`);
  await expect(page.locator('#pageMsg')).toContainText('Konfigurasi dimuat');
  await expect(page.locator('#tmStatus')).toContainText('Pesan tercatat');
}
// Keep readiness checks independent of the selected view: hidden data still loads.
export async function openDashboard(page, hash = '') {
  await page.goto(`/${hash}`);
  await expect(page.locator('#mdl')).toHaveValue('ui-model-a');
  await expect(page.locator('#sesiStat')).toContainText('Percakapan aktif');
}
export async function navigateSettings(page, hash) {
  await page.locator(`.settings-nav a[href="${hash}"]`).click();
  await expect.poll(() => new URL(page.url()).hash).toBe(hash);
  await expect(page.locator(hash)).toBeVisible();
}
// Navigate through the real shared shell; never unhide panels in test code.
export async function navigateDashboard(page, hash = '') {
  await page.locator(`.workspace-nav a[href="/${hash}"]`).click();
  await expect.poll(() => new URL(page.url()).hash).toBe(hash);
}
export async function expectDashboardView(page, hash = '') {
  const views = [
    ['', '#kpiActive'],
    ['#caseQueueTitle', '#caseQueueTitle'],
    ['#diagnosisPanel', '#diagnosisPanel'],
    ['#adminDrawer', '#adminDrawer'],
    ['#historySection', '#historySection'],
  ];
  for (const [route, selector] of views) {
    if (route === hash) await expect(page.locator(selector)).toBeVisible();
    else await expect(page.locator(selector)).toBeHidden();
  }
  const active = page.locator('.workspace-nav [aria-current="page"]');
  await expect(active).toHaveCount(1);
  await expect(active).toHaveAttribute('href', `/${hash}`);
}
export async function actionResponse(page, path, method, action) {
  const pending = page.waitForResponse(r => new URL(r.url()).pathname === path && r.request().method() === method)
    .then(async response => ({ response, body: path.startsWith('/api/auth/') ? null : await response.json(), sent: response.request().postDataJSON() }));
  const [result] = await Promise.all([pending, action()]);
  return result;
}
export async function configOf(request) {
  const response = await request.get('/api/status');
  expect(response.status()).toBe(200);
  return (await response.json()).config;
}
