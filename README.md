# setup-playwright

A CircleCI function that installs the browsers your project's [Playwright](https://playwright.dev)
needs, caching them across jobs, and runs your Playwright suite with `circleci testsuite`, which
splits it across parallel containers, stores the results and brings CircleCI's Smarter Testing to
it. The HTML report and the traces of failed tests are stored as artifacts.

```yaml
functions:
  node: github.com/circleci-functions/setup-node@v0.5.0
  playwright: github.com/circleci-functions/setup-playwright@v0.1.0

jobs:
  e2e:
    docker:
      - image: cimg/node:lts
    parallelism: 4
    environment:
      PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: "1"   # leave the browsers to setup-playwright
    steps:
      - checkout
      - node/install-packages
      - playwright:                           # same as playwright/run
          with:
            suite: e2e
            browsers: chromium
```

Run `circleci function add github.com/circleci-functions/setup-playwright --as playwright` to write
the entry.

## run

Installs the browsers as [`install`](#install) does, then runs `circleci testsuite run <suite>` in
the working directory.

| Flag | Default | Description |
|---|---|---|
| `suite` | | Required. The test suite to run |
| `base-url` | `$DEPLOYMENT_URL`, then `$VERCEL_DEPLOYMENT_URL` | Site to test, exported to the suite as `BASE_URL` |
| `run-tests` | `default` | Smarter Testing: which tests to run: `all`, `impacted`, `none` or `default` |
| `analyze-tests` | `default` | Smarter Testing: which tests to analyze for impact data: `all`, `impacted`, `none` or `default` |
| `trace` | `retain-on-failure` | Playwright's `--trace` mode for setup-playwright's own suite. Empty keeps the config's |
| `output-dir` | `test-results` | Playwright's `outputDir`, relative to `working-directory` |
| `working-directory` | `.` | Directory holding the project's `package.json`, with its dependencies installed |

Plus the [`install`](#install) flags.

Playwright does not read `BASE_URL` on its own, so point the config at it:

```js
use: { baseURL: process.env.BASE_URL },
```

A `base-url` without a scheme gets `https://`, as deploy steps often export a bare host name.

### The suite

If `.circleci/test-suites.yml` defines `suite`, testsuite runs it as written. Otherwise
setup-playwright adds its own suite under that name for the run, and removes it afterwards, leaving
the file as it was. That suite:

- asks Playwright for its test files (`playwright test --list`), so `testDir`, `testMatch` and
  `projects` are respected;
- runs them with the `junit` reporter, which testsuite reads, and the `html` reporter;
- records a trace of every failed test (`--trace=retain-on-failure`; change it with `trace`).

testsuite splits the test files across the job's `parallelism` by their past timings, and stores
the results in the job's Tests tab, so the function does neither itself. Rerunning a workflow's
failed tests from the CircleCI UI reruns only those files. The step fails if any test fails.

To change the suite, write your own: run `circleci testsuite generate e2e --template=playwright` and
edit it. Add `html` to its reporters (`--reporter=junit,html`) to keep the HTML report.

### Smarter Testing

`run-tests` and `analyze-tests` pass straight to testsuite. Left at `default`, testsuite runs every
test on the default branch and only the impacted ones elsewhere, and analyzes on the default branch
only. The opt-in features are enabled per suite, so define the suite in `.circleci/test-suites.yml`:

```yaml
---
name: e2e
discover: cd e2e && find . -type f -name '*.spec.*' | sed 's|^\./||'   # files relative to testDir
run: PLAYWRIGHT_JUNIT_OUTPUT_FILE="<< outputs.junit >>" npx playwright test --reporter=junit,html << test.atoms >>
analysis: npm run coverage:e2e -- << test.atoms >>   # yours: writes LCOV to << outputs.lcov >>
options:
  store-test-results: true
  test-impact-analysis: true      # run only the tests a change affects
  max-auto-rerun: 2               # rerun failed tests within the run
  dynamic-test-splitting: true    # hand out tests to containers as they free up
  full-test-run-paths:
    - package.json
    - playwright.config.*
```

Test impact analysis needs the `analysis` command to report which source files each test file
covers. CircleCI has no coverage plugin for Playwright, so instrument the app (for example with
`babel-plugin-istanbul`), collect the browser's coverage per test file, and write it as LCOV to
`<< outputs.lcov >>`. See CircleCI's
[test impact analysis guide](https://circleci.com/docs/guides/test/set-up-test-impact-analysis/).

Playwright's own `retries` and testsuite's `max-auto-rerun` both retry failures; use one.

### Artifacts

After the suite runs, whether it passed or not, the step stores as artifacts:

| Artifact | What |
|---|---|
| `playwright/report` | The HTML report, with the traces in it. setup-playwright sets `PLAYWRIGHT_HTML_OUTPUT_DIR`, so this holds for your own suites too when they use the `html` reporter |
| `playwright/test-results` | `output-dir`: traces, screenshots and videos Playwright kept |

## install

Installs the browsers that the project's `@playwright/test` needs and, with `with-deps`, their
system packages.

| Flag | Default | Description |
|---|---|---|
| `browsers` | Playwright's default set | Browsers to install, comma-separated: `chromium`, `chromium-headless-shell`, `firefox`, `webkit`, `chrome`, `msedge`, ... |
| `with-deps` | `true` | Also install the system packages the browsers need (with `sudo` on Linux). Set to `false` on an image that has them, such as `mcr.microsoft.com/playwright` |
| `only-shell` | `false` | Install only Chromium's headless shell, not full Chromium |
| `cache` | `true` | Restore the browsers from the job cache, and save them after a fresh download |
| `working-directory` | `.` | Directory holding the project's `package.json`, with its dependencies installed |

It uses the project's own Playwright from `node_modules`, so install your dependencies first; the
browser builds always match its version. Setting `PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: "1"` on the job
keeps the package install from downloading them too.

### Caching

Playwright says where each browser build goes (`playwright install --dry-run`), and those
directories are cached, keyed by the platform, the Playwright version and the directories
themselves. A change to other dependencies keeps the cached browsers; upgrading Playwright or
changing `browsers` caches the new set once. Only the builds in use are saved. System packages
cannot be cached, so `with-deps` installs them on every run. The browsers go where Playwright puts
them: `PLAYWRIGHT_BROWSERS_PATH` if set, otherwise the platform default.

## Platforms

Linux, macOS and Windows, on amd64 and arm64. `with-deps` installs system packages on Debian and
Ubuntu only, as Playwright does.

## Releasing

See [RELEASE.md](RELEASE.md).
