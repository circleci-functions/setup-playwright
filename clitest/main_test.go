// Package clitest_test exercises the setup-playwright CLI contract against a
// fake Playwright CLI in the project's node_modules and a fake circleci CLI:
// the commands it runs, the cache entries it saves and restores, the suite it
// hands testsuite and the artifacts it stores. Hermetic, so it runs under
// `task test`. The real Playwright and testsuite are covered by acceptance/.
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

// fakeTestsuite stands in for the circleci CLI's testsuite. It logs the
// arguments, directory and variables it was run with, and the suites file it
// would read, writes an HTML report where Playwright would, saves a trace into
// FAKE_TESTSUITE_TRACES if set, and exits with FAKE_TESTSUITE_EXIT.
const fakeTestsuite = `#!/bin/sh
{
  echo "args=$*"
  echo "dir=$(pwd -P)"
  echo "base=$BASE_URL"
  echo "open=$PLAYWRIGHT_HTML_OPEN"
} >> "$FAKE_TESTSUITE_LOG"
[ -z "$FAKE_SUITES_FILE" ] || cp "$FAKE_SUITES_FILE" "$FAKE_TESTSUITE_LOG.suites" 2>/dev/null
mkdir -p "$PLAYWRIGHT_HTML_OUTPUT_DIR" && echo '<html/>' > "$PLAYWRIGHT_HTML_OUTPUT_DIR/index.html"
if [ -n "$FAKE_TESTSUITE_TRACES" ]; then
  mkdir -p "$FAKE_TESTSUITE_TRACES/two-chromium" && echo zip > "$FAKE_TESTSUITE_TRACES/two-chromium/trace.zip"
fi
exit "${FAKE_TESTSUITE_EXIT:-0}"
`

const version = "1.65.1"

type env struct {
	sandbox   *sdktest.Sandbox
	project   string
	browsers  string
	log       string
	testsuite string
	vars      map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Playwright CLI is a shell script")
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	e := &env{
		sandbox:   sdktest.NewSandbox(t),
		project:   project,
		browsers:  filepath.Join(t.TempDir(), "ms-playwright"),
		log:       filepath.Join(t.TempDir(), "playwright.log"),
		testsuite: filepath.Join(t.TempDir(), "testsuite.log"),
	}
	// A checkout, so the project is the root testsuite reads its suites from.
	assert.NilError(t, os.Mkdir(filepath.Join(project, ".git"), 0o750))
	pkg := filepath.Join(project, "node_modules", "@playwright", "test")
	bin := filepath.Join(project, "node_modules", ".bin")
	assert.NilError(t, os.MkdirAll(pkg, 0o750))
	assert.NilError(t, os.MkdirAll(bin, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version": "`+version+`"}`), 0o600))
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "playwright"), []byte(fakePlaywright), 0o700)) //nolint:gosec // an executable script
	circleci := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(circleci, "circleci"), []byte(fakeTestsuite), 0o700)) //nolint:gosec // an executable script
	e.vars = map[string]string{
		"PLAYWRIGHT_BROWSERS_PATH": e.browsers,
		"FAKE_PLAYWRIGHT_LOG":      e.log,
		"FAKE_TESTSUITE_LOG":       e.testsuite,
		"FAKE_SUITES_FILE":         e.suitesFile(),
		"PATH":                     circleci + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	return e
}

func (e *env) suitesFile() string {
	return filepath.Join(e.project, ".circleci", "test-suites.yml")
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

// testsuiteRun returns what the fake testsuite was run with, or nil if it was
// not run.
func (e *env) testsuiteRun(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(e.testsuite)
	if os.IsNotExist(err) {
		return nil
	}
	assert.NilError(t, err)
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	return got
}

// suitesSeen returns the test-suites.yml the fake testsuite saw.
func (e *env) suitesSeen(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.testsuite + ".suites")
	assert.NilError(t, err)
	return string(b)
}

// artifacts returns the stored artifacts, destination to path.
func (e *env) artifacts() map[string]string {
	got := map[string]string{}
	for _, a := range e.sandbox.Cache.StoredArtifacts() {
		got[a.Destination] = a.Path
	}
	return got
}
