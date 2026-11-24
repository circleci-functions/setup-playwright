package suites_test

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci-functions/setup-playwright/suites"
)

// tree makes dir/sub/deeper and returns dir, resolved, as a temporary
// directory may be behind a symlink.
func tree(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	assert.NilError(t, os.MkdirAll(filepath.Join(dir, "sub", "deeper"), 0o750))
	return dir
}

func touch(t *testing.T, path string) {
	t.Helper()
	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	assert.NilError(t, os.WriteFile(path, []byte("---\nname: other\n"), 0o600))
}

// The cases are the ones checked against circleci-testsuite itself.
func TestFile(t *testing.T) {
	tests := []struct {
		name   string
		files  []string // made under the tree
		from   string
		want   string
		exists bool
	}{
		{"root of the repository wins", []string{".git/HEAD", ".circleci/test-suites.yml", "sub/.circleci/test-suites.yml"}, "sub", ".circleci/test-suites.yml", true},
		{"then the working directory", []string{".git/HEAD", "sub/.circleci/test-suites.yml"}, "sub", "sub/.circleci/test-suites.yml", true},
		{"not a directory in between", []string{".git/HEAD", "sub/.circleci/test-suites.yml"}, "sub/deeper", ".circleci/test-suites.yml", false},
		{"config.yml marks the root", []string{".circleci/config.yml", "sub/.circleci/test-suites.yml"}, "sub/deeper", ".circleci/test-suites.yml", false},
		{"git before config.yml", []string{".git/HEAD", "sub/.circleci/config.yml"}, "sub/deeper", ".circleci/test-suites.yml", false},
		{"nearest suites file with no other root", []string{".circleci/test-suites.yml", "sub/.circleci/test-suites.yml"}, "sub/deeper", "sub/.circleci/test-suites.yml", true},
		{"nowhere", nil, "sub/deeper", "sub/deeper/.circleci/test-suites.yml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tree(t)
			for _, f := range tt.files {
				touch(t, filepath.Join(dir, filepath.FromSlash(f)))
			}
			got, exists, err := suites.File(filepath.Join(dir, filepath.FromSlash(tt.from)))
			assert.NilError(t, err)
			assert.Check(t, cmp.Equal(got, filepath.Join(dir, filepath.FromSlash(tt.want))))
			assert.Check(t, cmp.Equal(exists, tt.exists))
		})
	}
}

func TestDefines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test-suites.yml")
	assert.NilError(t, os.WriteFile(path, []byte("---\nname: unit\ndiscover: x\n---\n# empty\n---\nname: e2e\n"), 0o600))

	for suite, want := range map[string]bool{"unit": true, "e2e": true, "smoke": false} {
		got, err := suites.Defines(path, suite)
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(got, want), suite)
	}
}

func TestDefines_MissingFile(t *testing.T) {
	got, err := suites.Defines(filepath.Join(t.TempDir(), "nope.yml"), "e2e")
	assert.NilError(t, err)
	assert.Check(t, !got)
}

func TestDefines_BadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test-suites.yml")
	assert.NilError(t, os.WriteFile(path, []byte("name: [unclosed\n"), 0o600))
	_, err := suites.Defines(path, "e2e")
	assert.Check(t, cmp.ErrorContains(err, path))
}

var suite = suites.Suite{Name: "e2e", Discover: "list", Run: "run << test.atoms >>", Options: suites.Options{StoreTestResults: true}}

func TestAdd_ToANewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".circleci", "test-suites.yml")

	restore, err := suites.Add(path, suite)
	assert.NilError(t, err)
	b, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(string(b), "---\nname: e2e\ndiscover: list\nrun: run << test.atoms >>\noptions:\n    store-test-results: true\n"))
	defined, err := suites.Defines(path, "e2e")
	assert.NilError(t, err)
	assert.Check(t, defined)

	assert.NilError(t, restore())
	_, err = os.Stat(filepath.Join(dir, ".circleci"))
	assert.Check(t, os.IsNotExist(err), "the .circleci directory Add made should go")
}

func TestAdd_KeepsAnExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, ".circleci", "config.yml"))
	path := filepath.Join(dir, ".circleci", "test-suites.yml")

	restore, err := suites.Add(path, suite)
	assert.NilError(t, err)
	assert.NilError(t, restore())
	_, err = os.Stat(path)
	assert.Check(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, ".circleci", "config.yml"))
	assert.Check(t, err)
}

func TestAdd_ToAnExistingFile(t *testing.T) {
	for name, original := range map[string]string{
		"ending in a newline": "---\nname: unit\n",
		"without one":         "name: unit",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test-suites.yml")
			assert.NilError(t, os.WriteFile(path, []byte(original), 0o640))

			restore, err := suites.Add(path, suite)
			assert.NilError(t, err)
			for _, s := range []string{"unit", "e2e"} {
				defined, err := suites.Defines(path, s)
				assert.NilError(t, err)
				assert.Check(t, defined, s)
			}

			assert.NilError(t, restore())
			b, err := os.ReadFile(path)
			assert.NilError(t, err)
			assert.Check(t, cmp.Equal(string(b), original))
		})
	}
}
