import { test, expect, openDashboard, openSettings } from '../support/fixtures.js';

test.use({ seedCases: true });

for (const viewport of [{ width: 1440, height: 900 }, { width: 768, height: 1024 }, { width: 390, height: 844 }]) {
  test(`responsive interactions ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport);
    const noOverflow = async () => {
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    };
    await openDashboard(page);
    await noOverflow();
    await page.locator('[data-case-id]').first().click();
    await expect(page.locator('#caseDetail')).toBeVisible();
    await noOverflow();
    const dialog = await page.locator('#caseDetail').boundingBox();
    expect(dialog.x).toBeGreaterThanOrEqual(0);
    expect(dialog.x + dialog.width).toBeLessThanOrEqual(viewport.width);
    await page.getByRole('button', { name: 'Tutup detail kasus' }).click();
    await expect(page.locator('#diagnosisPanel')).toBeHidden();
    await page.locator('#toggleDiagnosis').click();
    await expect(page.locator('#q')).toBeFocused();
    await expect(page.locator('#diagnosisPanel')).toBeVisible();
    await noOverflow();
    await page.locator('#toggleDiagnosis').click();
    await expect(page.locator('#diagnosisPanel')).toBeHidden();
    if (viewport.width === 1440) {
      const sidebar = await page.locator('.app-sidebar').boundingBox();
      const main = await page.locator('.cockpit-shell').boundingBox();
      expect(main.x).toBeGreaterThanOrEqual(sidebar.x + sidebar.width);
      await page.getByRole('link', { name: 'AI Diagnostics', exact: true }).click();
      await expect(page.locator('#diagnosisPanel')).toBeVisible();
    }
    await page.locator('#adminDrawer summary').click();
    await page.getByRole('button', { name: /Kelola Daftar Blokir/ }).click();
    await expect(page.locator('#blInput')).toBeFocused();
    await noOverflow();
    await page.getByRole('button', { name: 'Tutup Dialog' }).click();
    await openSettings(page);
    await noOverflow();
    await expect(page.locator('script[src="/noc-shell.js"]')).toHaveCount(1);
    await expect(page.locator('.app-sidebar')).toHaveCount(1);
    if (viewport.width === 1440) {
      await expect(page.locator('.app-sidebar')).toBeVisible();
      const sidebar = await page.locator('.app-sidebar').boundingBox();
      const main = await page.locator('#mainContent').boundingBox();
      expect(main.x).toBeGreaterThanOrEqual(sidebar.x + sidebar.width);
      expect(main.x + main.width).toBeLessThanOrEqual(viewport.width + 1);
    } else {
      // Shared shell becomes a horizontal, scrollable navigation strip.
      await expect(page.locator('.app-sidebar')).toBeVisible();
      await page.evaluate(() => scrollTo(0, 0));
      const sidebar = await page.locator('.app-sidebar').boundingBox();
      const main = await page.locator('#mainContent').boundingBox();
      expect(sidebar.x).toBeGreaterThanOrEqual(0);
      expect(sidebar.x + sidebar.width).toBeLessThanOrEqual(viewport.width + 1);
      expect(main.y).toBeGreaterThanOrEqual(sidebar.y + sidebar.height);
      await page.locator('.workspace-nav').getByRole('link', { name: 'Pengaturan', exact: true }).scrollIntoViewIfNeeded();
      await expect(page.locator('.workspace-nav [aria-current="page"]')).toHaveText('Pengaturan');
    }
    await page.locator('.settings-nav a[href="#policyCard"]').click();
    await expect(page.locator('.settings-nav a[href="#policyCard"]')).toHaveAttribute('aria-current', 'location');
    await page.getByRole('button', { name: 'Simpan Perubahan Aturan' }).click();
    await expect(page.locator('#adminDialog')).toBeVisible();
    await expect(page.locator('#adminNumber')).toBeFocused();
    await noOverflow();
    await page.keyboard.press('Escape');
    await expect(page.locator('#adminDialog')).toBeHidden();
    await page.goto('/login.html');
    await noOverflow();
    await page.locator('#p').fill('synthetic-test');
    await page.locator('#togglePassword').click();
    await expect(page.locator('#p')).toHaveAttribute('type', 'text');
    await expect(page.locator('#togglePassword')).toHaveAttribute('aria-pressed', 'true');
    await page.locator('#togglePassword').click();
    await expect(page.locator('#p')).toHaveAttribute('type', 'password');
  });
}
