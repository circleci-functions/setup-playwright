// testDir is not Playwright's default, so the suite has to ask Playwright
// where the tests are rather than assume tests/.
const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: 'e2e',
  reporter: 'list',
  use: { baseURL: process.env.BASE_URL },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
});
