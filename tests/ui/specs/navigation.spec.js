import { test, expect, openDashboard, openSettings, navigateDashboard, navigateSettings, expectDashboardView, actionResponse } from '../support/fixtures.js';

const dashboardHashes = ['', '#caseQueueTitle', '#diagnosisPanel', '#adminDrawer', '#historySection'];
const settingsViews = {
  '#conversation': ['conversation', 'reasoning', 'aiSaveBar'],
  '#agent': ['agent', 'aiSaveBar'],
  '#integrations': ['integrations'],
  '#teamCard': ['teamCard'],
  '#staffCard': ['staffCard'],
  '#policyCard': ['policyCard'],
  '#recipesCard': ['recipesCard'],
};
async function expectSettingsView(page, hash) {
  const visible = settingsViews[hash];
  for (const id of [...new Set(Object.values(settingsViews).flat())]) {
    const section = page.locator(`.settings-content > section#${id}`);
    if (visible.includes(id)) await expect(section).toBeVisible();
    else await expect(section).toBeHidden();
  }
  const active = page.locator('.settings-nav [aria-current="page"]');
  await expect(active).toHaveCount(1);
  await expect(active).toHaveAttribute('href', hash);
  await expect(page.locator('.workspace-nav [aria-current="page"]')).toHaveText('Pengaturan');
  // One shared AI save bar, not duplicate controls in each endpoint/agent section.
  await expect(page.locator('#aiSaveBar button[onclick="saveSettings()"]')).toHaveCount(1);
  await expect(page.locator('button[onclick="saveSettings()"]:visible')).toHaveCount(visible.includes('aiSaveBar') ? 1 : 0);
}

for (const width of [1440, 768, 390]) {
  test(`sidebar routes are exclusive with back/forward at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openDashboard(page);
    await expectDashboardView(page);
    for (const hash of dashboardHashes.slice(1)) {
      await navigateDashboard(page, hash);
      await expectDashboardView(page, hash);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    }
    for (const hash of [...dashboardHashes].reverse().slice(1)) {
      await page.goBack();
      await expect.poll(() => new URL(page.url()).hash).toBe(hash);
      await expectDashboardView(page, hash);
    }
    await page.goForward();
    await expectDashboardView(page, '#caseQueueTitle');
    // Re-selecting the current destination must not toggle it closed.
    await navigateDashboard(page, '#caseQueueTitle');
    await expectDashboardView(page, '#caseQueueTitle');
  });
}

test('dashboard direct hashes and reload retain only the selected view', async ({ page }) => {
  for (const hash of dashboardHashes) {
    await openDashboard(page, hash);
    await expectDashboardView(page, hash);
    await page.reload();
    await expectDashboardView(page, hash);
  }
});

test('diagnosis and case drafts survive sidebar and history navigation', async ({ page }) => {
  await openDashboard(page, '#diagnosisPanel');
  await page.locator('#q').fill('Unsaved diagnosis draft');
  await page.locator('#tgt').fill('ui-customer-draft');
  await navigateDashboard(page, '#caseQueueTitle');
  await page.locator('#caseSearch').fill('unsaved case search');
  await page.locator('#caseFilter').selectOption('attention');
  await navigateDashboard(page, '#adminDrawer');
  await page.locator('#waTo').fill('628120000123');
  await page.locator('#waMsg').fill('Unsent draft');
  await navigateDashboard(page);
  await navigateDashboard(page, '#diagnosisPanel');
  await expect(page.locator('#q')).toHaveValue('Unsaved diagnosis draft');
  await expect(page.locator('#tgt')).toHaveValue('ui-customer-draft');
  await navigateDashboard(page, '#caseQueueTitle');
  await expect(page.locator('#caseSearch')).toHaveValue('unsaved case search');
  await expect(page.locator('#caseFilter')).toHaveValue('attention');
  await navigateDashboard(page, '#adminDrawer');
  await expect(page.locator('#waTo')).toHaveValue('628120000123');
  await expect(page.locator('#waMsg')).toHaveValue('Unsent draft');
  await page.goBack();
  await expectDashboardView(page, '#caseQueueTitle');
  await expect(page.locator('#caseSearch')).toHaveValue('unsaved case search');
});

test('settings tabs scope sections and save controls through clicks, history and reload', async ({ page }) => {
  await openSettings(page);
  await expectSettingsView(page, '#conversation');
  for (const hash of Object.keys(settingsViews)) {
    await navigateSettings(page, hash);
    await expectSettingsView(page, hash);
  }
  await page.goBack();
  await expectSettingsView(page, '#policyCard');
  await page.goForward();
  await expectSettingsView(page, '#recipesCard');
  for (const hash of Object.keys(settingsViews)) {
    await openSettings(page, hash);
    await expectSettingsView(page, hash);
    await page.reload();
    await expectSettingsView(page, hash);
  }
});

test('settings drafts persist between exclusive tabs and save together from agent', async ({ page }) => {
  await openSettings(page);
  await page.locator('#conversation #sBase').fill('http://127.0.0.1:1/unsaved-a');
  await page.locator('#reasoning #sCodexBase').fill('http://127.0.0.1:1/unsaved-b');
  await navigateSettings(page, '#agent');
  await page.locator('#agent #sSteps').fill('7');
  await page.locator('#agent #greetingTemplate').fill('Draft {{name}}');
  await navigateSettings(page, '#integrations');
  await page.locator('#integrations #billingUrl').fill('http://127.0.0.1:1/unsaved-billing');
  await navigateSettings(page, '#staffCard');
  await page.locator('#staffCard #stfName').fill('Unsaved staff draft');
  await navigateSettings(page, '#conversation');
  await expect(page.locator('#conversation #sBase')).toHaveValue('http://127.0.0.1:1/unsaved-a');
  await expect(page.locator('#reasoning #sCodexBase')).toHaveValue('http://127.0.0.1:1/unsaved-b');
  await page.goBack();
  await expect(page.locator('#staffCard #stfName')).toHaveValue('Unsaved staff draft');
  await page.goForward();
  await expectSettingsView(page, '#conversation');
  await navigateSettings(page, '#integrations');
  await expect(page.locator('#integrations #billingUrl')).toHaveValue('http://127.0.0.1:1/unsaved-billing');
  await navigateSettings(page, '#agent');
  await expect(page.locator('#agent #sSteps')).toHaveValue('7');
  await expect(page.locator('#agent #greetingTemplate')).toHaveValue('Draft {{name}}');
  const saved = await actionResponse(page, '/api/config', 'POST', () => page.locator('#aiSaveBar').getByRole('button', { name: 'Simpan Endpoint & Perilaku Agen' }).click());
  expect(saved.sent).toMatchObject({ max_steps: 7, greeting_template: 'Draft {{name}}', llm_base_url: 'http://127.0.0.1:1/unsaved-a', codex_base_url: 'http://127.0.0.1:1/unsaved-b' });
  expect(saved.sent).not.toHaveProperty('billing_url');
  expect(saved.sent).not.toHaveProperty('staff_members');
  await expect(page.locator('#aiSaveBar #setMsg')).toContainText('Tersimpan');
});
