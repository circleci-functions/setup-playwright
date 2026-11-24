//go:build acceptance

// Package acceptance_test runs setup-playwright against the real Playwright
// npm package and its browsers, downloaded from npm and Playwright's CDN, and
// the real circleci testsuite. Tagged out of `task test` because it needs
// Node.js and the network.
package acceptance_test

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// binaryPathEnvVar names a prebuilt setup-playwright to test, for an
// executor with no Go toolchain to compile one with.
const binaryPathEnvVar = "SETUP_PLAYWRIGHT_BINARY"

// version is the Playwright the test project pins.
const version = "1.65.1"

// The test project is embedded, so a cross-compiled suite carries it to an
// executor with no checkout.
//
//go:embed testdata/project
var testdata embed.FS

var binaryPath string

// sharedBrowsers is a browsers folder the tests share, so that they download
// Chromium once between them.
var sharedBrowsers string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "setup-playwright-acceptance-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	sharedBrowsers = filepath.Join(dir, "ms-playwright")

	if bin := os.Getenv(binaryPathEnvVar); bin != "" {
		abs, err := filepath.Abs(bin)
		if err != nil {
			return 0, fmt.Errorf("%s=%q: %w", binaryPathEnvVar, bin, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return 0, fmt.Errorf("%s=%q: %w", binaryPathEnvVar, bin, err)
		}
		binaryPath = abs
		return m.Run(), nil
	}

	p := compiler.NewParallel(1)
	defer p.Cleanup()
	p.Add(compiler.Work{Result: &binaryPath, Name: "setup-playwright", Target: "..", Source: "."})
	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// env is a test's sandbox, the test project with its dependencies installed,
// and the variables a job would set.
type env struct {
	sandbox *sdktest.Sandbox
	project string
	vars    map[string]string
}

func newEnv(t *testing.T, browsers string) *env {
	t.Helper()
	e := &env{
		sandbox: sdktest.NewSandbox(t),
		project: project(t),
	}
	e.vars = map[string]string{
		"PLAYWRIGHT_BROWSERS_PATH": browsers,
		// A sandbox gives each run a fresh HOME, where Node.js and the
		// circleci CLI keep what they have installed.
		"npm_config_cache": npmCache(t),
		"XDG_DATA_HOME":    xdgData(t),
		"CI":               "true",
	}
	if runtime.GOOS == "windows" {
		for _, name := range windowsProfileEnvVars {
			e.vars[name] = os.Getenv(name)
		}
	}
	return e
}

// windowsProfileEnvVars are the variables Windows gives every process, which
// a sandbox does not pass on and Node.js fails to start without.
var windowsProfileEnvVars = []string{
	"APPDATA", "LOCALAPPDATA", "USERPROFILE", "SystemRoot", "SystemDrive",
	"windir", "TEMP", "TMP", "ComSpec", "PATHEXT", "ProgramData", "ProgramFiles",
}

func (e *env) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	vars := e.sandbox.Env()
	maps.Copy(vars, e.vars)
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(vars))
	_, err := r.Run(append(args, "--working-directory="+e.project)...)
	return r.StdoutString() + r.StderrString(), err
}

// project copies the test project, installs its dependencies and makes it a
// git repository, as a checkout is, since testsuite finds the project's root
// by its .git.
func project(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	sub, err := fs.Sub(testdata, "testdata/project")
	assert.NilError(t, err)
	assert.NilError(t, os.CopyFS(dir, sub))
	for _, args := range [][]string{
		{npm(), "install", "--no-audit", "--no-fund", "--loglevel=error"},
		{"git", "init", "--quiet"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1")
		out, err := cmd.CombinedOutput()
		assert.NilError(t, err, string(out))
	}
	return dir
}

// needTestsuite skips a test that runs the suite where the circleci CLI has
// no testsuite, which a CircleCI job always has.
func needTestsuite(t *testing.T) {
	t.Helper()
	if err := exec.Command("circleci", "testsuite", "--help").Run(); err != nil {
		if sdk.InCircleCIJob() {
			t.Fatalf("circleci testsuite is not available in this job: %v", err)
		}
		t.Skipf("circleci testsuite is not available: %v (install it with `circleci extension install testsuite`)", err)
	}
}

// suite names a test's suite uniquely within a workflow. testsuite takes a
// second run of a suite in a workflow as a rerun, and skips the tests that
// passed the first time, so the tests and the jobs running them each need
// their own.
func suite(name string) string {
	if job := os.Getenv("CIRCLE_JOB"); job != "" {
		return name + "-" + job
	}
	return name
}

func npmCache(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(npm(), "config", "get", "cache").Output()
	assert.NilError(t, err)
	return string(trimNewline(out))
}

// xdgData is where the circleci CLI found its extensions before the sandbox
// moved HOME.
func xdgData(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	assert.NilError(t, err)
	return filepath.Join(home, ".local", "share")
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func npm() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}
