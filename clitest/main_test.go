// Package clitest_test exercises the setup-playwright CLI contract against a
// fake Playwright CLI in the project's node_modules: the commands it runs and
// the cache entries it saves and restores. Hermetic, so it runs under
// `task test`. The real Playwright is covered by acceptance/.
package clitest_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

var binaryPath string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	p := compiler.NewParallel(1)
	defer p.Cleanup()
	p.Add(compiler.Work{Result: &binaryPath, Name: "setup-playwright", Target: "..", Source: "."})
	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// fakePlaywright stands in for the Playwright npm package's CLI. It logs each
// call, with whether the browsers were already there. `install --dry-run`
// lists a location per browser named (chromium by default) plus ffmpeg, and
// `install` makes each one, marker and all.
const fakePlaywright = `#!/bin/sh
root="${PLAYWRIGHT_BROWSERS_PATH:-$HOME/.cache/ms-playwright}"
present=absent
[ -f "$root/chromium-1255/INSTALLATION_COMPLETE" ] && present=present
echo "$* $present" >> "$FAKE_PLAYWRIGHT_LOG"
[ "$1" = install ] || exit 0
shift
dry=""; browsers=""
for a in "$@"; do
  case "$a" in
    --dry-run) dry=1 ;;
    --*) ;;
    *) browsers="$browsers $a" ;;
  esac
done
for b in ${browsers:-chromium} ffmpeg; do
  dir="$root/$b-1255"
  if [ -n "$dry" ]; then
    printf '%s\n  Install location:    %s\n\n' "$b" "$dir"
  else
    [ -z "$FAKE_PLAYWRIGHT_INSTALL_FAILS" ] || { echo "install failed" >&2; exit 1; }
    mkdir -p "$dir" && touch "$dir/INSTALLATION_COMPLETE"
  fi
done
`

const version = "1.65.1"

type env struct {
	sandbox  *sdktest.Sandbox
	project  string
	browsers string
	log      string
	vars     map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Playwright CLI is a shell script")
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	e := &env{
		sandbox:  sdktest.NewSandbox(t),
		project:  project,
		browsers: filepath.Join(t.TempDir(), "ms-playwright"),
		log:      filepath.Join(t.TempDir(), "playwright.log"),
	}
	pkg := filepath.Join(project, "node_modules", "@playwright", "test")
	bin := filepath.Join(project, "node_modules", ".bin")
	assert.NilError(t, os.MkdirAll(pkg, 0o750))
	assert.NilError(t, os.MkdirAll(bin, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version": "`+version+`"}`), 0o600))
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "playwright"), []byte(fakePlaywright), 0o700)) //nolint:gosec // an executable script
	e.vars = map[string]string{
		"PLAYWRIGHT_BROWSERS_PATH": e.browsers,
		"FAKE_PLAYWRIGHT_LOG":      e.log,
	}
	return e
}

func (e *env) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	vars := e.sandbox.Env()
	maps.Copy(vars, e.vars)
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(vars))
	_, err := r.Run(append(args, "--working-directory="+e.project)...)
	return r.StdoutString() + r.StderrString(), err
}

// calls returns the fake Playwright CLI's log, one call per line, and
// empties it.
func (e *env) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(e.log)
	if os.IsNotExist(err) {
		return nil
	}
	assert.NilError(t, err)
	assert.NilError(t, os.Remove(e.log))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}
