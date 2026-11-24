package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-playwright/playwright"
)

// installFlags say which browsers to install, for both commands.
type installFlags struct {
	WorkingDirectory string `name:"working-directory" default:"." help:"Directory holding the project's package.json, with @playwright/test installed in its node_modules." example:"apps/web"`
	Browsers         string `help:"Browsers to install, comma-separated, as playwright install names them: chromium, chromium-headless-shell, firefox, webkit, or a branded browser such as chrome or msedge. Empty installs Playwright's default set." example:"chromium,firefox"`
	WithDeps         bool   `name:"with-deps" default:"true" help:"Also install the operating system packages the browsers need, with sudo on Linux. Set to false on an image that has them, such as mcr.microsoft.com/playwright."`
	OnlyShell        bool   `name:"only-shell" help:"Install only Chromium's headless shell, not full Chromium. Smaller, but headed runs and the chrome channel need full Chromium."`
	Cache            bool   `default:"true" help:"Restore the browsers from the job cache, and save them after a fresh download. Set to false to always download them."`
}

func (f installFlags) browsers() playwright.Browsers {
	var names []string
	for _, n := range strings.FieldsFunc(f.Browsers, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		names = append(names, strings.TrimSpace(n))
	}
	return playwright.Browsers{Names: names, OnlyShell: f.OnlyShell}
}

type installCmd struct {
	installFlags `embed:""`
}

func (c *installCmd) Run(ctx context.Context, client *sdk.Client) error {
	_, err := install(ctx, client, c.installFlags)
	return err
}

// install makes sure the browsers the project's Playwright needs are in
// Playwright's browsers folder, restoring them from the job cache or
// downloading them. `playwright install` runs even after a restore: it
// installs the system packages, which cannot be cached, and finishes any
// browser whose download was cut short, while leaving a complete one alone.
func install(ctx context.Context, client *sdk.Client, f installFlags) (*playwright.Project, error) {
	p, err := playwright.Open(f.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	b := f.browsers()
	locations, err := p.Locations(ctx, b)
	if err != nil {
		return nil, err
	}
	key := browsersCacheKey(p.Version, locations)

	source := "already installed"
	if f.Cache && !allInstalled(locations) {
		_, err := client.RestoreCache(ctx, []string{key}, sdk.WithStepName("Restoring Playwright's browsers"))
		sdk.LogCacheErr("restore", err)
		source = "restored from the cache"
	}
	downloaded := !allInstalled(locations)
	if downloaded {
		source = "downloaded"
	}
	if err := p.Install(ctx, b, f.WithDeps); err != nil {
		return nil, err
	}
	if f.Cache && downloaded {
		sdk.LogCacheErr("save", client.SaveCache(ctx, key, locations, sdk.WithStepName("Saving Playwright's browsers")))
	}

	fmt.Fprintf(os.Stderr, "setup-playwright: %s %s browsers %s:\n", p.Package, p.Version, source)
	for _, l := range locations {
		fmt.Fprintf(os.Stderr, "  %s\n", l)
	}
	return p, nil
}

// browsersCacheKey names the cache entry for one set of browser builds on
// one platform. The install locations name each build and its revision, and a
// cache restores to the absolute paths it was saved from, so they are the key:
// a different browser set, Playwright upgrade or PLAYWRIGHT_BROWSERS_PATH
// makes a new entry, and only the builds in use are saved.
func browsersCacheKey(version string, locations []string) string {
	sum := sha256.Sum256([]byte(strings.Join(locations, "\n")))
	return fmt.Sprintf("setup-playwright-browsers-v1-%s-%s-%s", sdk.OSArch(), version, hex.EncodeToString(sum[:4]))
}

func allInstalled(locations []string) bool {
	for _, l := range locations {
		if !playwright.Installed(l) {
			return false
		}
	}
	return true
}
