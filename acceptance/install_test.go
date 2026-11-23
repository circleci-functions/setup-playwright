//go:build acceptance

package acceptance_test

import (
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// chromium installs the smallest browser set the project's tests run on.
var chromium = []string{"--browsers=chromium", "--only-shell", "--with-deps=false"}

func TestInstall_DownloadsTheBrowsers(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "ms-playwright"))

	out, err := e.run(t, append([]string{"install"}, chromium...)...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "@playwright/test "+version+" browsers downloaded"))
	entries, err := filepath.Glob(filepath.Join(e.vars["PLAYWRIGHT_BROWSERS_PATH"], "chromium_headless_shell-*", "INSTALLATION_COMPLETE"))
	assert.NilError(t, err)
	assert.Check(t, cmp.Len(entries, 1), out)
	assert.Check(t, cmp.Len(e.sandbox.Cache.SavedKeys(), 1))

	out, err = e.run(t, append([]string{"install"}, chromium...)...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers already installed"))
}
