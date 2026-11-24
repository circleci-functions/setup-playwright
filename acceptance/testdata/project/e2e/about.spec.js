const { test, expect } = require('@playwright/test');

test('about is reachable', async ({ page }) => {
  const res = await page.goto('/about');
  expect(res.ok()).toBeTruthy();
});
