//go:build acceptance

// Package acceptance_test runs setup-playwright against the real Playwright
// npm package and its browsers, downloaded from npm and Playwright's CDN.
// Tagged out of `task test` because it needs Node.js and the network.
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

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {

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
		// A sandbox gives each run a fresh HOME, where npm keeps its cache.
		"npm_config_cache": npmCache(t),
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

// project copies the test project and installs its dependencies, with the
// browser download skipped, for setup-playwright to do.
func project(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	sub, err := fs.Sub(testdata, "testdata/project")
	assert.NilError(t, err)
	assert.NilError(t, os.CopyFS(dir, sub))
	for _, args := range [][]string{
		{npm(), "install", "--no-audit", "--no-fund", "--loglevel=error"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1")
		out, err := cmd.CombinedOutput()
		assert.NilError(t, err, string(out))
	}
	return dir
}

func npmCache(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(npm(), "config", "get", "cache").Output()
	assert.NilError(t, err)
	return string(trimNewline(out))
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
