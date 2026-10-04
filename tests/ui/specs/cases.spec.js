import { test, expect, openDashboard } from '../support/fixtures.js';
import { unverifiedKPI } from '../support/schemas.js';

test.describe('authentic Go case store projections', () => {
  test.use({ seedCases: true });
  test('search, all filters, details, evidence, Escape and close', async ({ page, request }) => {
    await openDashboard(page);
    const response = await request.get('/api/cases');
    const { cases, count } = await response.json();
    expect(count).toBe(5);
    expect(cases).toHaveLength(5);
    await expect(page.locator('[data-case-id]')).toHaveCount(5);
    for (const [filter, expected] of [['active', 1], ['attention', 3], ['resolved', 1], ['all', 5]]) {
      await page.locator('#caseFilter').selectOption(filter);
      await expect(page.locator('[data-case-id]')).toHaveCount(expected);
    }
    await page.locator('#caseSearch').fill('UI-CUSTOMER-4');
    await expect(page.locator('[data-case-id]')).toHaveCount(1);
    const c = cases.find(c => c.state === 'RESOLVED');
    expect(c.verifications[0]).toMatchObject({ passed: true, source: 'fixture-probe', summary: 'Verified <b>literal</b>' });
    const trigger = page.locator(`[data-case-id="${c.case_id}"]`);
    await trigger.click();
    await expect(page.locator('#caseDetail')).toBeVisible();
    await expect(page.locator('#caseDetailTitle')).toHaveText(c.case_id);
    await expect(page.locator('#caseDetailBody')).toContainText('Recorded <b>evidence</b>');
    await expect(page.locator('#caseDetailBody')).toContainText('Verified <b>literal</b>');
    await expect(page.locator('#caseDetailBody b')).toHaveCount(0);
    await page.keyboard.press('Escape');
    await expect(page.locator('#caseDetail')).toBeHidden();
    await expect(trigger).toBeFocused();
    await trigger.click();
    await page.getByRole('button', { name: 'Tutup detail kasus' }).click();
    await expect(page.locator('#caseDetail')).toBeHidden();
    await page.locator('#caseSearch').fill('not-present');
    await expect(page.locator('#caseRows')).toContainText('Tidak ada kasus sesuai');
    await page.locator('#caseSearch').fill('');
    await page.locator('#refreshDashboard').click();
    await expect(page.locator('[data-case-id]')).toHaveCount(5);
    await expect(page.locator('#refreshDashboard')).toBeEnabled();
  });
});

test('KPI zero verified is not replaced by resolved; invalid types are unavailable', async ({ page }) => {
  // Deliberate schema edge cases. Other specs exercise the actual Go response.
  await page.route('**/api/kpi', route => route.fulfill({ json: unverifiedKPI }));
  await openDashboard(page);
  await expect(page.locator('#kpiResolved')).toHaveText('0');
  await expect(page.locator('#kpiActive')).toHaveText('3');
  for (const key of ['active_cases', 'escalated', 'verified_resolved', 'escalation_rate_percent', 'total_cases']) {
    await page.route('**/api/kpi', route => route.fulfill({ json: { ...unverifiedKPI, [key]: '3' } }));
    await page.locator('#refreshDashboard').click();
    await expect(page.locator('#kpiActive')).toHaveText('—');
    await expect(page.locator('#obsStat')).toContainText('KPI tidak lengkap');
    await expect(page.locator('#refreshDashboard')).toBeEnabled();
  }
});

test('case API failure is visible and refresh recovers', async ({ page }) => {
  await page.route('**/api/cases', route => route.fulfill({ status: 503, json: { error: 'fixture unavailable' } }));
  await openDashboard(page);
  await expect(page.locator('#caseRows')).toContainText('Data kasus tidak tersedia');
  await page.unroute('**/api/cases');
  await page.locator('#refreshDashboard').click();
  await expect(page.locator('#caseRows')).toContainText('Belum ada kasus tercatat');
});
