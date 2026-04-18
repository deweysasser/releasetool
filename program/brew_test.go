package program

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/releasetool/homebrew"
	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockGithub stands up an httptest.Server and swaps the homebrew package's
// GitHub client constructor so Brew.Run talks to the mock. Cleanup restores
// the original on test exit.
func newMockGithub(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	restore := homebrew.SetNewGithubClientForTest(func() *github.Client {
		c := github.NewClient(nil)
		u, err := url.Parse(server.URL + "/")
		require.NoError(t, err)
		c.BaseURL = u
		c.UploadURL = u
		return c
	})
	t.Cleanup(restore)
	return server
}

// TestBrew_Run_GeneratesDefaultAndVersionedFormulas drives the full Brew.Run
// flow against a mocked GitHub with 3 releases (2 stable + 1 rc) and asserts
// the set of output files and their contents. A re-run is then verified to
// be a no-op.
func TestBrew_Run_GeneratesDefaultAndVersionedFormulas(t *testing.T) {
	mux := http.NewServeMux()
	server := newMockGithub(t, mux)

	mux.HandleFunc("/repos/deweysasser/cumulus", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"description": "A better AWS CLI", "private": false}`)
	})
	// All three releases on a single page; no assets so no SHA256 downloads
	// are triggered during template rendering.
	mux.HandleFunc("/repos/deweysasser/cumulus/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
			{"tag_name": "v1.2.0-rc1", "prerelease": true,  "assets": []},
			{"tag_name": "v1.2.0",     "prerelease": false, "assets": []},
			{"tag_name": "v1.1.0",     "prerelease": false, "assets": []}
		]`)
		_ = server // keep reference to avoid unused-var churn if the signature shifts
	})

	dir := t.TempDir()
	t.Chdir(dir)

	b := &Brew{Repo: []string{"deweysasser/cumulus"}}
	require.NoError(t, b.Run(&Options{}))

	// Expect 4 formulas: 3 versioned + 1 default.
	want := []string{
		"cumulus.rb",
		"cumulus@1.2.0.rb",
		"cumulus@1.1.0.rb",
		"cumulus@1.2.0-rc1.rb",
	}
	for _, name := range want {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "missing %s", name)
		assert.False(t, info.IsDir())
	}

	// No stray .tmp files left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".tmp"),
			"stale tmp file left behind: %s", e.Name())
	}

	// Default formula content: newest STABLE, not the RC.
	defaultBody := readAll(t, filepath.Join(dir, "cumulus.rb"))
	assert.Contains(t, defaultBody, "class Cumulus < Formula")
	assert.Contains(t, defaultBody, `version "v1.2.0"`)

	// Versioned RC formula content.
	rcBody := readAll(t, filepath.Join(dir, "cumulus@1.2.0-rc1.rb"))
	assert.Contains(t, rcBody, "class CumulusAT120rc1 < Formula")
	assert.Contains(t, rcBody, `version "v1.2.0-rc1"`)

	// Stable versioned.
	stableBody := readAll(t, filepath.Join(dir, "cumulus@1.2.0.rb"))
	assert.Contains(t, stableBody, "class CumulusAT120 < Formula")

	// Re-run: should be a complete no-op — existing files untouched.
	statBefore := statAll(t, dir, want)
	require.NoError(t, b.Run(&Options{}))
	statAfter := statAll(t, dir, want)
	for i, name := range want {
		assert.Equal(t, statBefore[i].ModTime(), statAfter[i].ModTime(),
			"file %s was rewritten on a re-run but should be immutable/stable", name)
	}
}

// TestBrew_Run_SkipsDefaultWhenOnlyPrereleases verifies that a repo with only
// prerelease tags produces only versioned formulas — no default unversioned
// file is written.
func TestBrew_Run_SkipsDefaultWhenOnlyPrereleases(t *testing.T) {
	mux := http.NewServeMux()
	newMockGithub(t, mux)

	mux.HandleFunc("/repos/o/tool", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/tool/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name": "v1.0.0-rc2", "prerelease": true, "assets": []},
			{"tag_name": "v1.0.0-rc1", "prerelease": true, "assets": []}
		]`)
	})

	dir := t.TempDir()
	t.Chdir(dir)

	b := &Brew{Repo: []string{"o/tool"}}
	require.NoError(t, b.Run(&Options{}))

	_, err := os.Stat(filepath.Join(dir, "tool.rb"))
	assert.True(t, os.IsNotExist(err),
		"default tool.rb must NOT be written when the repo has only prereleases")

	for _, name := range []string{"tool@1.0.0-rc1.rb", "tool@1.0.0-rc2.rb"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.NoError(t, err, "missing versioned prerelease file: %s", name)
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func statAll(t *testing.T, dir string, names []string) []os.FileInfo {
	t.Helper()
	out := make([]os.FileInfo, len(names))
	for i, n := range names {
		info, err := os.Stat(filepath.Join(dir, n))
		require.NoError(t, err)
		out[i] = info
	}
	return out
}
