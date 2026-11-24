// Package playwright drives the Playwright a project has installed in its
// node_modules: its version, where its browsers go, and installing them.
package playwright

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// packages are the npm packages that carry the Playwright CLI, in the order
// they are looked for: the test runner, then the library on its own.
var packages = []string{"@playwright/test", "playwright"}

// Project is a directory with Playwright installed in its node_modules.
type Project struct {
	// Dir is the project's absolute path.
	Dir string
	// Package is the npm package Playwright came from.
	Package string
	// Version is the installed package's version, which pins the browser
	// builds it needs.
	Version string
	// Stdout and Stderr receive the output of the Playwright commands run.
	Stdout, Stderr io.Writer
}

// Open returns the project in dir. It fails if Playwright is not installed
// there, since the browsers it needs, and the commands that install them,
// both come from the npm package.
func Open(dir string) (*Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		manifest := filepath.Join(abs, "node_modules", filepath.FromSlash(pkg), "package.json")
		b, err := os.ReadFile(manifest) //nolint:gosec // the project's own package
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var m struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", manifest, err)
		}
		if m.Version == "" {
			return nil, fmt.Errorf("%s has no version", manifest)
		}
		p := &Project{Dir: abs, Package: pkg, Version: m.Version, Stdout: os.Stdout, Stderr: os.Stderr}
		if _, err := os.Stat(p.Bin()); err != nil {
			return nil, fmt.Errorf("%s %s is installed in %s but its CLI is missing: %w", pkg, m.Version, abs, err)
		}
		return p, nil
	}
	return nil, fmt.Errorf("playwright is not installed in %s: add @playwright/test to the project and install its dependencies (npm ci, pnpm install, yarn install) before this step", abs)
}

// Browsers says which browsers to install.
type Browsers struct {
	// Names are Playwright's browser names, such as chromium or firefox.
	// None installs Playwright's default set.
	Names []string
	// OnlyShell installs Chromium's headless shell without full Chromium.
	OnlyShell bool
}

func (b Browsers) args() []string {
	var args []string
	if b.OnlyShell {
		args = append(args, "--only-shell")
	}
	return append(args, b.Names...)
}

// Locations returns the directories the browsers install into, one per
// browser build, sorted. Playwright works them out from its version, the
// platform and PLAYWRIGHT_BROWSERS_PATH, so it is asked rather than
// second-guessed.
func (p *Project) Locations(ctx context.Context, b Browsers) ([]string, error) {
	var out bytes.Buffer
	cmd := p.Command(ctx, append([]string{"install", "--dry-run"}, b.args()...)...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("playwright install --dry-run: %w", err)
	}
	locations := ParseLocations(out.String())
	if len(locations) == 0 {
		return nil, errors.New("playwright install --dry-run listed no install locations")
	}
	return locations, nil
}

// ParseLocations returns the install locations in the output of
// `playwright install --dry-run`, sorted and without repeats.
func ParseLocations(out string) []string {
	var locations []string
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		_, location, ok := strings.Cut(s.Text(), "Install location:")
		if location = strings.TrimSpace(location); ok && location != "" {
			locations = append(locations, location)
		}
	}
	slices.Sort(locations)
	return slices.Compact(locations)
}

// Installed reports whether a browser build finished installing into
// location. Playwright writes the marker last, so a directory without it is
// a download that was cut short, which `playwright install` replaces.
func Installed(location string) bool {
	_, err := os.Stat(filepath.Join(location, "INSTALLATION_COMPLETE"))
	return err == nil
}

// installAttempts is how many times Install tries before giving up. Package
// mirrors and download CDNs fail now and then (an apt mirror mid-sync answers
// 503), and a retry a little later usually gets through.
const installAttempts = 3

// RetryDelayEnvVar overrides the pause before Install's first retry, as a Go
// duration. Tests set it so a failing install does not take half a minute.
const RetryDelayEnvVar = "SETUP_PLAYWRIGHT_RETRY_DELAY"

// Install downloads the browsers that are not installed already and, with
// withDeps, the operating system packages they need. It tries again after a
// failure, waiting longer each time, since `playwright install` picks up where
// a failed attempt stopped.
func (p *Project) Install(ctx context.Context, b Browsers, withDeps bool) error {
	args := []string{"install"}
	if withDeps {
		args = append(args, "--with-deps")
	}
	delay := retryDelay()
	var err error
	for attempt := 1; ; attempt++ {
		err = p.Command(ctx, append(args, b.args()...)...).Run()
		if err == nil || attempt == installAttempts || ctx.Err() != nil {
			break
		}
		fmt.Fprintf(p.Stderr, "setup-playwright: playwright %s failed (attempt %d of %d), trying again in %s\n",
			strings.Join(args, " "), attempt, installAttempts, delay)
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
		delay *= 2
	}
	if err != nil {
		return fmt.Errorf("playwright %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func retryDelay() time.Duration {
	if d, err := time.ParseDuration(os.Getenv(RetryDelayEnvVar)); err == nil && d >= 0 {
		return d
	}
	return 15 * time.Second
}

// Command runs the project's own Playwright CLI, never one npx would
// download.
func (p *Project) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, p.Bin(), args...) //nolint:gosec // the project's own Playwright
	cmd.Dir = p.Dir
	cmd.Stdout = p.Stdout
	cmd.Stderr = p.Stderr
	return cmd
}

// Bin is the project's Playwright CLI.
func (p *Project) Bin() string {
	name := "playwright"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	return filepath.Join(p.Dir, "node_modules", ".bin", name)
}
