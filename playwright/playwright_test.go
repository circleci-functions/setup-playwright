package playwright_test

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci-functions/setup-playwright/playwright"
)

const dryRun = `Chrome for Testing 159.0.8154.6 (playwright chromium v1255)
  Install location:    /home/circleci/.cache/ms-playwright/chromium-1255
  Download url:        https://cdn.playwright.dev/builds/cft/159.0.8154.6/linux64/chrome-linux64.zip

Chrome Headless Shell 159.0.8154.6 (playwright chromium-headless-shell v1255)
  Install location:    /home/circleci/.cache/ms-playwright/chromium_headless_shell-1255
  Download url:        https://cdn.playwright.dev/builds/cft/159.0.8154.6/linux64/chrome-headless-shell-linux64.zip

FFmpeg (playwright ffmpeg v1013)
  Install location:    /home/circleci/.cache/ms-playwright/ffmpeg-1013
  Download url:        https://cdn.playwright.dev/dbazure/download/playwright/builds/ffmpeg/1013/ffmpeg-linux.zip
  Download fallback 1: https://playwright.download.prss.microsoft.com/dbazure/download/playwright/builds/ffmpeg/1013/ffmpeg-linux.zip

FFmpeg (playwright ffmpeg v1013)
  Install location:    /home/circleci/.cache/ms-playwright/ffmpeg-1013
`

func TestParseLocations(t *testing.T) {
	assert.Check(t, cmp.DeepEqual(playwright.ParseLocations(dryRun), []string{
		"/home/circleci/.cache/ms-playwright/chromium-1255",
		"/home/circleci/.cache/ms-playwright/chromium_headless_shell-1255",
		"/home/circleci/.cache/ms-playwright/ffmpeg-1013",
	}))
}

func TestParseLocations_Windows(t *testing.T) {
	out := "Firefox 159.0 (playwright firefox v1572)\r\n  Install location:    C:\\Users\\circleci\\AppData\\Local\\ms-playwright\\firefox-1572\r\n"
	assert.Check(t, cmp.DeepEqual(playwright.ParseLocations(out), []string{
		`C:\Users\circleci\AppData\Local\ms-playwright\firefox-1572`,
	}))
}

func TestParseLocations_None(t *testing.T) {
	assert.Check(t, cmp.Len(playwright.ParseLocations("nothing to install\n"), 0))
}

func TestOpen_NotInstalled(t *testing.T) {
	_, err := playwright.Open(t.TempDir())
	assert.Check(t, cmp.ErrorContains(err, "playwright is not installed"))
}

func TestOpen_PrefersTheTestRunner(t *testing.T) {
	dir := t.TempDir()
	writePackage(t, dir, "@playwright/test", "1.65.1")
	writePackage(t, dir, "playwright", "1.65.1")
	writeBin(t, dir)

	p, err := playwright.Open(dir)
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(p.Package, "@playwright/test"))
	assert.Check(t, cmp.Equal(p.Version, "1.65.1"))
}

func TestOpen_FallsBackToTheLibrary(t *testing.T) {
	dir := t.TempDir()
	writePackage(t, dir, "playwright", "1.64.0")
	writeBin(t, dir)

	p, err := playwright.Open(dir)
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(p.Package, "playwright"))
	assert.Check(t, cmp.Equal(p.Version, "1.64.0"))
}

func TestOpen_NoVersion(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "node_modules", "@playwright", "test")
	assert.NilError(t, os.MkdirAll(pkg, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{}`), 0o600))

	_, err := playwright.Open(dir)
	assert.Check(t, cmp.ErrorContains(err, "has no version"))
}

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	assert.Check(t, !playwright.Installed(dir))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "INSTALLATION_COMPLETE"), nil, 0o600))
	assert.Check(t, playwright.Installed(dir))
}

func writePackage(t *testing.T, dir, name, version string) {
	t.Helper()
	pkg := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
	assert.NilError(t, os.MkdirAll(pkg, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version": "`+version+`"}`), 0o600))
}

func writeBin(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, "node_modules", ".bin")
	assert.NilError(t, os.MkdirAll(bin, 0o750))
	name := "playwright"
	if os.PathSeparator == '\\' {
		name += ".cmd"
	}
	assert.NilError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // an executable script
}
