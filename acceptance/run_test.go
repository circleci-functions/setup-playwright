//go:build acceptance

package acceptance_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// site serves a home page whose heading is heading, which the test project's
// spec expects to be "home", and an about page.
func site(t *testing.T, heading string) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "<html><body><h1>%s</h1></body></html>", heading)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

func TestRun_PassingSuite(t *testing.T) {
	needTestsuite(t)
	e := newEnv(t, sharedBrowsers)

	out, err := e.run(t, append([]string{"--suite=" + suite("passing"), "--base-url=" + site(t, "home")}, chromium...)...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "Ran 2 test atoms"))
	assert.Check(t, cmp.Contains(e.storedArtifacts(), "playwright/report"))
	_, err = os.Stat(filepath.Join(e.project, ".circleci"))
	assert.Check(t, os.IsNotExist(err), "the suite setup-playwright added should be gone")
}

func TestRun_FailingSuiteKeepsTraces(t *testing.T) {
	needTestsuite(t)
	e := newEnv(t, sharedBrowsers)

	out, err := e.run(t, append([]string{"--suite=" + suite("failing"), "--base-url=" + site(t, "away")}, chromium...)...)
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "home.spec.js"))
	traces, err := filepath.Glob(filepath.Join(e.project, "test-results", "*", "trace.zip"))
	assert.NilError(t, err)
	assert.Check(t, cmp.Len(traces, 1), out)
	stored := e.storedArtifacts()
	assert.Check(t, cmp.Contains(stored, "playwright/report"))
	assert.Check(t, cmp.Contains(stored, "playwright/test-results"))
}

func TestRun_PassesArgs(t *testing.T) {
	needTestsuite(t)
	e := newEnv(t, sharedBrowsers)

	out, err := e.run(t, append([]string{"--suite=" + suite("args"), "--base-url=" + site(t, "home"),
		"--args=--project chromium --grep 'home has'"}, chromium...)...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "Ran 1 test atoms"), "the grep should narrow the listing as well as the run")
}

func TestRun_SelectsNoTests(t *testing.T) {
	needTestsuite(t)
	e := newEnv(t, sharedBrowsers)

	out, err := e.run(t, append([]string{"--suite=" + suite("none"), "--run-tests=none", "--analyze-tests=none"}, chromium...)...)
	assert.NilError(t, err, out)
	assert.Check(t, !strings.Contains(out, "Ran 2 test atoms"), out)
}

func (e *env) storedArtifacts() map[string]bool {
	got := map[string]bool{}
	for _, a := range e.sandbox.Cache.StoredArtifacts() {
		got[a.Destination] = true
	}
	return got
}
