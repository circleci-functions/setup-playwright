// Package suites finds and edits the .circleci/test-suites.yml that
// `circleci testsuite run` reads, so a run can bring its own suite when the
// project does not define one.
package suites

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// name is where testsuite reads suites from, relative to a directory.
var name = filepath.Join(".circleci", "test-suites.yml")

// markers find the project's root, in the order testsuite tries them: each
// is looked for up the whole tree before the next.
var markers = []string{".git", filepath.Join(".circleci", "config.yml"), name}

// File returns the test-suites.yml that testsuite reads for a run in dir,
// and whether it exists. testsuite reads the one at the project's root, or
// failing that the one in dir itself; when neither exists, the file at the
// root is where a suite should go, so that is returned.
func File(dir string) (path string, exists bool, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", false, err
	}
	root := dir
	for _, m := range markers {
		if found, ok := nearest(dir, m); ok {
			root = found
			break
		}
	}
	for _, d := range []string{root, dir} {
		p := filepath.Join(d, name)
		if isFile(p) {
			return p, true, nil
		}
	}
	return filepath.Join(root, name), false, nil
}

// nearest returns the closest of dir and its parents that holds marker.
func nearest(dir, marker string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Defines reports whether the test-suites.yml at path defines the suite
// called suite. A missing file defines nothing.
func Defines(path, suite string) (bool, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the project's own config
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	for {
		var doc struct {
			Name string `yaml:"name"`
		}
		err := d.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		if doc.Name == suite {
			return true, nil
		}
	}
}

// Suite is one test suite, as test-suites.yml writes it.
type Suite struct {
	Name     string  `yaml:"name"`
	Discover string  `yaml:"discover"`
	Run      string  `yaml:"run"`
	Options  Options `yaml:"options"`
}

// Options are a suite's options.
type Options struct {
	StoreTestResults bool `yaml:"store-test-results"`
}

// Add appends suite to the test-suites.yml at path, creating it if need be,
// and returns a function that puts the file back as it was: the original
// bytes, or no file at all, and no .circleci directory if Add made it.
func Add(path string, suite Suite) (restore func() error, err error) {
	doc, err := yaml.Marshal(suite)
	if err != nil {
		return nil, err
	}
	original, err := os.ReadFile(path) //nolint:gosec // the project's own config
	switch {
	case err == nil:
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		content := append(bytes.Clone(original), separator(original)...)
		if err := os.WriteFile(path, append(content, doc...), info.Mode().Perm()); err != nil { //nolint:gosec // the project's own config
			return nil, err
		}
		return func() error { return os.WriteFile(path, original, info.Mode().Perm()) }, nil //nolint:gosec // the project's own config
	case errors.Is(err, fs.ErrNotExist):
		dir := filepath.Dir(path)
		_, statErr := os.Stat(dir)
		madeDir := errors.Is(statErr, fs.ErrNotExist)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, append([]byte("---\n"), doc...), 0o600); err != nil {
			return nil, err
		}
		return func() error {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if madeDir {
				_ = os.Remove(dir) // only if empty: something else may have written there since
			}
			return nil
		}, nil
	default:
		return nil, err
	}
}

// separator starts a new YAML document after existing content.
func separator(existing []byte) []byte {
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		return []byte("\n---\n")
	}
	return []byte("---\n")
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
