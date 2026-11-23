# setup-playwright

A CircleCI function that installs the browsers your project's [Playwright](https://playwright.dev)
needs, and their system packages, caching the browsers across jobs.

```yaml
functions:
  node: github.com/circleci-functions/setup-node@v0.5.0
  playwright: github.com/circleci-functions/setup-playwright@v0.1.0

jobs:
  e2e:
    docker:
      - image: cimg/node:lts
    environment:
      PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: "1"   # leave the browsers to setup-playwright
    steps:
      - checkout
      - node/install-packages
      - playwright/install:
          with:
            browsers: chromium
      - run: npx playwright test
```

Run `circleci function add github.com/circleci-functions/setup-playwright --as playwright` to write
the entry.

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
