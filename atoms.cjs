// Prints the test files in a `playwright test --list --reporter=json` report,
// one per line, relative to their testDir as Playwright reports them. It is
// setup-playwright's suite's discover command, so it fails if the listing did.
const fs = require('fs');

let report;
try {
  report = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
} catch (e) {
  console.error(`setup-playwright: playwright test --list wrote no report: ${e.message}`);
  process.exit(1);
}
const errors = report.errors || [];
for (const e of errors) console.error(e.message || JSON.stringify(e));
if (errors.length) process.exit(1);

const files = new Set();
for (const s of report.suites || []) {
  if (s.file) files.add(s.file.replace(/\\/g, '/'));
}
for (const f of [...files].sort()) console.log(f);
