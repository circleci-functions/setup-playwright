const { test, expect } = require('@playwright/test');

test('home has its heading', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('h1')).toHaveText('home', { timeout: 5000 });
});
