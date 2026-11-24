package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/google/shlex"

	"github.com/circleci-functions/setup-playwright/suites"
)

// traceModes are the values playwright test's --trace takes.
var traceModes = []string{"on", "off", "on-first-retry", "on-all-retries", "retain-on-failure", "retain-on-first-failure"}

type runCmd struct {
	installFlags `embed:""`

	Suite        string `required:"" help:"Test suite to run with circleci testsuite. If .circleci/test-suites.yml defines it, it runs as defined; otherwise setup-playwright's Playwright suite runs under this name, with every test file Playwright finds and the JUnit and HTML reporters." example:"e2e"`
	BaseURL      string `name:"base-url" help:"URL of the site to test, exported to the suite as BASE_URL. Empty uses DEPLOYMENT_URL, then VERCEL_DEPLOYMENT_URL, which a deploy step exports, and with neither set leaves BASE_URL alone." example:"https://my-app.example.com"`
	RunTests     string `name:"run-tests" enum:"default,all,impacted,none" default:"default" help:"Smarter Testing: which test files to select and run. impacted runs only those a change affects, which needs test impact analysis set up for the suite; default leaves it to testsuite: all on the default branch, impacted elsewhere."`
	AnalyzeTests string `name:"analyze-tests" enum:"default,all,impacted,none" default:"default" help:"Smarter Testing: which test files to analyze for impact data, with the suite's analysis command. default leaves it to testsuite: impacted on the default branch, none elsewhere."`
	Trace        string `default:"retain-on-failure" help:"Playwright's --trace mode for setup-playwright's own suite: on, off, on-first-retry, on-all-retries, retain-on-failure or retain-on-first-failure. Empty keeps the Playwright config's. A suite defined in .circleci/test-suites.yml sets its own."`
	Args         string `help:"More arguments for playwright test in setup-playwright's own suite, used both to list the tests and to run them, such as --project or --config. A suite defined in .circleci/test-suites.yml sets its own." example:"--project chromium"`
	OutputDir    string `name:"output-dir" default:"test-results" help:"Playwright's outputDir, relative to the working directory, where it saves traces, screenshots and videos. Stored as artifacts after the run."`
}

func (c *runCmd) Run(ctx context.Context, client *sdk.Client) error {
	if c.Trace != "" && !slices.Contains(traceModes, c.Trace) {
		return fmt.Errorf("--trace=%q: want one of %s, or empty", c.Trace, strings.Join(traceModes, ", "))
	}
	extra, err := c.extraArgs()
	if err != nil {
		return err
	}
	testsuite, err := exec.LookPath("circleci")
	if err != nil {
		return errors.New("the circleci CLI is not on PATH: run this step in a CircleCI job, where it is installed with testsuite")
	}
	p, err := install(ctx, client, c.installFlags)
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "setup-playwright-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	report := filepath.Join(tmp, "report")

	restore, err := c.useSuite(p.Dir, tmp, extra)
	if err != nil {
		return err
	}
	defer func() {
		if err := restore(); err != nil {
			fmt.Fprintf(os.Stderr, "setup-playwright: could not restore .circleci/test-suites.yml: %v\n", err)
		}
	}()

	env := append(os.Environ(), "PLAYWRIGHT_HTML_OUTPUT_DIR="+report, "PLAYWRIGHT_HTML_OPEN=never")
	if base := baseURL(c.BaseURL); base != "" {
		env = append(env, "BASE_URL="+base)
		fmt.Fprintf(os.Stderr, "setup-playwright: testing %s\n", base)
	}

	args := []string{"testsuite", "run", c.Suite}
	if c.RunTests != "default" {
		args = append(args, "--run-tests="+c.RunTests)
	}
	if c.AnalyzeTests != "default" {
		args = append(args, "--analyze-tests="+c.AnalyzeTests)
	}
	cmd := exec.CommandContext(ctx, testsuite, args...) //nolint:gosec // the job's own CLI
	cmd.Dir = p.Dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Cancelling the step stops testsuite gently first, so it can stop
	// Playwright and report what ran.
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = 20 * time.Second

	err = cmd.Run()
	// The step may have been cancelled, and what was saved is still worth
	// keeping. By default Playwright keeps traces only of failures.
	storeCtx := context.WithoutCancel(ctx)
	store(storeCtx, client, report, "playwright/report")
	store(storeCtx, client, inDir(p.Dir, c.OutputDir), "playwright/test-results")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("circleci testsuite run %q failed (exit %d)", c.Suite, exitErr.ExitCode())
	}
	return err
}

// useSuite makes sure the test-suites.yml testsuite reads for a run in dir
// defines the suite, adding setup-playwright's own if it does not, and
// returns what puts the file back.
func (c *runCmd) useSuite(dir, tmp string, extra []string) (restore func() error, err error) {
	path, exists, err := suites.File(dir)
	if err != nil {
		return nil, err
	}
	if exists {
		defined, err := suites.Defines(path, c.Suite)
		if err != nil {
			return nil, err
		}
		if defined {
			fmt.Fprintf(os.Stderr, "setup-playwright: running %s as %s defines it\n", c.Suite, path)
			if len(extra) > 0 {
				fmt.Fprintf(os.Stderr, "setup-playwright: ignoring --args, since %s sets the suite's own commands\n", path)
			}
			return func() error { return nil }, nil
		}
	}
	s, err := c.ownSuite(tmp, extra)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "setup-playwright: %s is not in %s, so running setup-playwright's Playwright suite as %s, and removing it afterwards\n", c.Suite, path, c.Suite)
	return suites.Add(path, s)
}

// listAtoms prints the test files in a `playwright test --list
// --reporter=json` report.
//
//go:embed atoms.cjs
var listAtoms []byte

// ownSuite is the suite run when the project does not define one. testsuite's
// built-in Playwright suite looks for tests only under tests/ and passes
// --reporter=junit, which replaces the config's reporters and so drops the
// HTML report. This one asks Playwright for its test files, so testDir,
// testMatch and projects are respected, and adds the HTML reporter.
func (c *runCmd) ownSuite(tmp string, extra []string) (suites.Suite, error) {
	script := filepath.Join(tmp, "atoms.cjs")
	if err := os.WriteFile(script, listAtoms, 0o600); err != nil {
		return suites.Suite{}, err
	}
	list := filepath.Join(tmp, "list.json")
	playwright := "npx --no-install playwright test"
	for _, a := range extra {
		playwright += " " + shellQuote(a)
	}
	run := "PLAYWRIGHT_JUNIT_OUTPUT_FILE=\"<< outputs.junit >>\" " + playwright + " --reporter=junit,html"
	if c.Trace != "" {
		run += " --trace=" + c.Trace
	}
	return suites.Suite{
		Name: c.Suite,
		// Playwright reports each file relative to its testDir, as it
		// takes them back in the run command. The list goes to a file so
		// that nothing the config prints can be taken for a test.
		Discover: fmt.Sprintf("PLAYWRIGHT_JSON_OUTPUT_FILE=%s %s --list --reporter=json >/dev/null; node %s %s",
			quote(list), playwright, quote(script), quote(list)),
		Run:     run + " << test.atoms >>",
		Options: suites.Options{StoreTestResults: true},
	}, nil
}

// quote single-quotes a path for the shell testsuite runs commands with,
// with forward slashes, which that shell takes on Windows too.
func quote(path string) string {
	return shellQuote(filepath.ToSlash(path))
}

// shellQuote single-quotes s for the shell testsuite runs commands with.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ownedArgs are the playwright test options setup-playwright's suite sets
// itself, or that would fight testsuite: it lists the tests, picks the
// reporters, and splits the tests across parallel containers.
var ownedArgs = map[string]string{
	"--list":     "the suite lists the tests itself",
	"--reporter": "the suite needs the junit reporter for testsuite, and adds html",
	"--shard":    "testsuite splits the tests across the job's parallelism",
	"--trace":    "use the trace flag",
}

// extraArgs splits Args the way a shell would, and rejects the options the
// suite sets itself.
func (c *runCmd) extraArgs() ([]string, error) {
	args, err := shlex.Split(c.Args)
	if err != nil {
		return nil, fmt.Errorf("--args: %w", err)
	}
	for _, a := range args {
		name, _, _ := strings.Cut(a, "=")
		if why, ok := ownedArgs[name]; ok {
			return nil, fmt.Errorf("--args: %s is not allowed: %s", name, why)
		}
	}
	return args, nil
}

// store keeps a file, or a directory with anything in it, as artifacts.
// Outside a job, or if the store fails, it says so and carries on: a failed
// store should not hide the suite's own result.
func store(ctx context.Context, client *sdk.Client, path, destination string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		if entries, err := os.ReadDir(path); err != nil || len(entries) == 0 {
			return
		}
	}
	err = client.StoreArtifacts(ctx, path, destination)
	if err != nil && !errors.Is(err, sdk.ErrNoServer) {
		fmt.Fprintf(os.Stderr, "setup-playwright: could not store %s: %v\n", destination, err)
	}
}

// inDir resolves path against dir, as Playwright resolves its outputDir
// against the project.
func inDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// deploymentURLEnvVars are where deploy steps leave the URL they deployed to,
// in the order they are tried: the name deploy functions share, then the one
// setup-vercel exports.
var deploymentURLEnvVars = []string{"DEPLOYMENT_URL", "VERCEL_DEPLOYMENT_URL"}

// baseURL returns the site to test, with a scheme, or "" to leave BASE_URL
// as the job has it.
func baseURL(flag string) string {
	base := strings.TrimSpace(flag)
	for _, name := range deploymentURLEnvVars {
		if base != "" {
			break
		}
		base = strings.TrimSpace(os.Getenv(name))
	}
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return strings.TrimRight(base, "/")
}

func interrupt(p *os.Process) error {
	if runtime.GOOS == "windows" {
		return p.Kill()
	}
	return p.Signal(syscall.SIGINT)
}
