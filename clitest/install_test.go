package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestInstall_DownloadsAndCachesTheBrowsers(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "install")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "@playwright/test "+version+" browsers downloaded"))
	assert.Check(t, cmp.Contains(out, filepath.Join(e.browsers, "chromium-1255")))
	assert.Check(t, cmp.DeepEqual(e.calls(t), []string{
		"install --dry-run absent",
		"install --with-deps absent",
	}))
	keys := e.sandbox.Cache.SavedKeys()
	assert.Assert(t, cmp.Len(keys, 1))
	assert.Check(t, strings.HasPrefix(keys[0], "setup-playwright-browsers-v1-"), keys[0])
	assert.Check(t, cmp.Contains(keys[0], "-"+version+"-"))
}

func TestInstall_RestoresTheBrowsersFromTheCache(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, "install")
	assert.NilError(t, err, out)
	e.calls(t)
	assert.NilError(t, os.RemoveAll(e.browsers))

	out, err = e.run(t, "install")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers restored from the cache"))
	assert.Check(t, cmp.DeepEqual(e.calls(t), []string{
		"install --dry-run absent",
		"install --with-deps present",
	}))
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 1))
}

func TestInstall_LeavesInstalledBrowsersAlone(t *testing.T) {
	e := newEnv(t)
	for _, b := range []string{"chromium-1255", "ffmpeg-1255"} {
		assert.NilError(t, os.MkdirAll(filepath.Join(e.browsers, b), 0o750))
		assert.NilError(t, os.WriteFile(filepath.Join(e.browsers, b, "INSTALLATION_COMPLETE"), nil, 0o600))
	}

	out, err := e.run(t, "install")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers already installed"))
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 0))
}

func TestInstall_RedownloadsACutShortBrowser(t *testing.T) {
	e := newEnv(t)
	assert.NilError(t, os.MkdirAll(filepath.Join(e.browsers, "chromium-1255"), 0o750))

	out, err := e.run(t, "install")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers downloaded"))
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 1))
}

func TestInstall_PassesTheBrowsersAndOptions(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "install", "--browsers=chromium, firefox", "--only-shell", "--with-deps=false")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.DeepEqual(e.calls(t), []string{
		"install --dry-run --only-shell chromium firefox absent",
		"install --only-shell chromium firefox absent",
	}))
}

func TestInstall_KeysTheCacheByTheBrowsers(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, "install", "--browsers=chromium")
	assert.NilError(t, err, out)

	out, err = e.run(t, "install", "--browsers=chromium,webkit")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers downloaded"))
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 2))
}

func TestInstall_WithoutTheCache(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "install", "--cache=false")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers downloaded"))
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 0))
}

func TestInstall_FailsWhenPlaywrightDoes(t *testing.T) {
	e := newEnv(t)
	e.vars["FAKE_PLAYWRIGHT_INSTALL_FAILS"] = "1"

	out, err := e.run(t, "install")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "playwright install --with-deps:"))
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 0))
}

func TestInstall_NeedsTheProjectsDependencies(t *testing.T) {
	e := newEnv(t)
	assert.NilError(t, os.RemoveAll(filepath.Join(e.project, "node_modules")))

	out, err := e.run(t, "install")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "playwright is not installed in "+e.project))
}
