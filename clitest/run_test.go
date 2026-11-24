package clitest_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestRun_InstallsThenRunsTheSuite(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "run", "--suite=e2e")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "browsers downloaded"))
	assert.Check(t, cmp.DeepEqual(e.testsuiteRun(t), map[string]string{
		"args": "testsuite run e2e",
		"dir":  e.project,
		"base": "",
		"open": "never",
	}))
}

func TestRun_IsTheDefaultCommand(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Equal(e.testsuiteRun(t)["args"], "testsuite run e2e"))
}

func TestRun_BringsItsOwnSuiteAndRemovesIt(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "running setup-playwright's Playwright suite as e2e"))
	seen := e.suitesSeen(t)
	assert.Check(t, cmp.Contains(seen, "name: e2e\n"))
	assert.Check(t, cmp.Contains(seen, "npx --no-install playwright test --list --reporter=json"))
	assert.Check(t, cmp.Contains(seen, `PLAYWRIGHT_JUNIT_OUTPUT_FILE="<< outputs.junit >>" npx --no-install playwright test --reporter=junit,html --trace=retain-on-failure << test.atoms >>`))
	assert.Check(t, cmp.Contains(seen, "store-test-results: true"))
	_, err = os.Stat(filepath.Join(e.project, ".circleci"))
	assert.Check(t, os.IsNotExist(err), "the suites file and its directory should be gone")
}

func TestRun_KeepsTheConfigsTrace(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e", "--trace=")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(e.suitesSeen(t), "--reporter=junit,html << test.atoms >>"))
}

func TestRun_PassesArgsToListingAndRunning(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e", "--args=--project chromium --config 'e2e/my config.ts'")
	assert.NilError(t, err, out)
	seen := e.suitesSeen(t)
	want := `npx --no-install playwright test '--project' 'chromium' '--config' 'e2e/my config.ts'`
	assert.Check(t, cmp.Contains(seen, want+" --list --reporter=json"))
	assert.Check(t, cmp.Contains(seen, want+" --reporter=junit,html --trace=retain-on-failure << test.atoms >>"))
}

func TestRun_RejectsArgsTheSuiteSets(t *testing.T) {
	for _, args := range []string{"--reporter=list", "--shard 1/2", "--trace on", "--list"} {
		t.Run(args, func(t *testing.T) {
			e := newEnv(t)

			out, err := e.run(t, "--suite=e2e", "--args="+args)
			assert.Check(t, err != nil, out)
			assert.Check(t, cmp.Contains(out, "is not allowed"))
			assert.Check(t, cmp.Nil(e.testsuiteRun(t)))
		})
	}
}

func TestRun_IgnoresArgsForTheProjectsSuite(t *testing.T) {
	e := newEnv(t)
	own := "---\nname: e2e\ndiscover: echo a.spec.ts\nrun: npx playwright test << test.atoms >>\n"
	assert.NilError(t, os.MkdirAll(filepath.Dir(e.suitesFile()), 0o750))
	assert.NilError(t, os.WriteFile(e.suitesFile(), []byte(own), 0o600))

	out, err := e.run(t, "--suite=e2e", "--args=--project chromium")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "ignoring --args"))
	assert.Check(t, cmp.Equal(e.suitesSeen(t), own))
}

func TestRun_RejectsAnUnknownTrace(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e", "--trace=sometimes")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, `--trace="sometimes"`))
	assert.Check(t, cmp.Nil(e.testsuiteRun(t)))
}

func TestRun_UsesTheProjectsSuite(t *testing.T) {
	e := newEnv(t)
	own := "---\nname: e2e\ndiscover: find e2e -name '*.spec.ts'\nrun: npx playwright test << test.atoms >>\noptions:\n  test-impact-analysis: true\n"
	assert.NilError(t, os.MkdirAll(filepath.Dir(e.suitesFile()), 0o750))
	assert.NilError(t, os.WriteFile(e.suitesFile(), []byte(own), 0o600))

	out, err := e.run(t, "--suite=e2e")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "running e2e as "+e.suitesFile()+" defines it"))
	assert.Check(t, cmp.Equal(e.suitesSeen(t), own))
	b, err := os.ReadFile(e.suitesFile())
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(string(b), own))
}

func TestRun_AddsToAndRestoresTheProjectsSuites(t *testing.T) {
	e := newEnv(t)
	own := "---\nname: unit\ndiscover: echo unit\nrun: 'true'\n"
	assert.NilError(t, os.MkdirAll(filepath.Dir(e.suitesFile()), 0o750))
	assert.NilError(t, os.WriteFile(e.suitesFile(), []byte(own), 0o600))
	e.vars["FAKE_TESTSUITE_EXIT"] = "1"

	out, err := e.run(t, "--suite=e2e")
	assert.Check(t, err != nil, out)
	seen := e.suitesSeen(t)
	assert.Check(t, cmp.Contains(seen, "name: unit\n"))
	assert.Check(t, cmp.Contains(seen, "name: e2e\n"))
	b, err := os.ReadFile(e.suitesFile())
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(string(b), own), "restored even though the suite failed")
}

func TestRun_PassesSmarterTestingSelection(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e", "--run-tests=impacted", "--analyze-tests=none")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Equal(e.testsuiteRun(t)["args"], "testsuite run e2e --run-tests=impacted --analyze-tests=none"))
}

func TestRun_RejectsAnUnknownSelection(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e", "--run-tests=some")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "--run-tests"))
	assert.Check(t, cmp.Nil(e.testsuiteRun(t)))
}

func TestRun_StoresTheReportAndTracesOfAFailedRun(t *testing.T) {
	e := newEnv(t)
	e.vars["FAKE_TESTSUITE_TRACES"] = filepath.Join(e.project, "test-results")
	e.vars["FAKE_TESTSUITE_EXIT"] = "1"

	out, err := e.run(t, "--suite=e2e")
	assert.Check(t, err != nil, out)
	got := e.artifacts()
	assert.Check(t, cmp.Contains(got, "playwright/report"))
	assert.Check(t, cmp.Equal(got["playwright/test-results"], filepath.Join(e.project, "test-results")))
}

func TestRun_StoresTracesFromTheConfiguredOutputDir(t *testing.T) {
	e := newEnv(t)
	e.vars["FAKE_TESTSUITE_TRACES"] = filepath.Join(e.project, "out", "e2e")

	out, err := e.run(t, "--suite=e2e", "--output-dir=out/e2e")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Equal(e.artifacts()["playwright/test-results"], filepath.Join(e.project, "out", "e2e")))
}

func TestRun_StoresNoTracesWhenThereAreNone(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--suite=e2e")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.DeepEqual(keys(e.artifacts()), []string{"playwright/report"}))
}

func TestRun_FailsWhenTheSuiteFails(t *testing.T) {
	e := newEnv(t)
	e.vars["FAKE_TESTSUITE_EXIT"] = "3"

	out, err := e.run(t, "--suite=e2e")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, `circleci testsuite run "e2e" failed (exit 3)`))
}

func TestRun_NeedsASuite(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t)
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "--suite"))
	assert.Check(t, cmp.Nil(e.testsuiteRun(t)))
}

func TestRun_NeedsTheCircleCICLI(t *testing.T) {
	e := newEnv(t)
	e.vars["PATH"] = t.TempDir()

	out, err := e.run(t, "--suite=e2e")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "the circleci CLI is not on PATH"))
	assert.Check(t, cmp.Nil(e.calls(t)), "nothing should be installed")
}

func TestRun_DoesNotRunTheSuiteWhenInstallFails(t *testing.T) {
	e := newEnv(t)
	e.vars["FAKE_PLAYWRIGHT_INSTALL_FAILS"] = "3"

	out, err := e.run(t, "--suite=e2e")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Nil(e.testsuiteRun(t)))
}

func TestRun_PassesTheBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://app.example.com":   "https://app.example.com",
		"https://app.example.com/":  "https://app.example.com",
		"app.example.com":           "https://app.example.com",
		"http://localhost:3000/app": "http://localhost:3000/app",
	}
	for flag, want := range tests {
		t.Run(flag, func(t *testing.T) {
			e := newEnv(t)

			out, err := e.run(t, "--suite=e2e", "--base-url="+flag)
			assert.NilError(t, err, out)
			assert.Check(t, cmp.Equal(e.testsuiteRun(t)["base"], want))
			assert.Check(t, cmp.Contains(out, "testing "+want))
		})
	}
}

func TestRun_DefaultsToTheDeployment(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		flag string
		want string
	}{
		{"deployment", map[string]string{"DEPLOYMENT_URL": "https://deploy.example.com"}, "", "https://deploy.example.com"},
		{"vercel deployment", map[string]string{"VERCEL_DEPLOYMENT_URL": "my-app-abc123.vercel.app"}, "", "https://my-app-abc123.vercel.app"},
		{"deployment first", map[string]string{"DEPLOYMENT_URL": "https://deploy.example.com", "VERCEL_DEPLOYMENT_URL": "my-app.vercel.app"}, "", "https://deploy.example.com"},
		{"flag first", map[string]string{"DEPLOYMENT_URL": "https://deploy.example.com"}, "https://flag.example.com", "https://flag.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			maps.Copy(e.vars, tt.vars)

			args := []string{"--suite=e2e"}
			if tt.flag != "" {
				args = append(args, "--base-url="+tt.flag)
			}
			out, err := e.run(t, args...)
			assert.NilError(t, err, out)
			assert.Check(t, cmp.Equal(e.testsuiteRun(t)["base"], tt.want))
		})
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
