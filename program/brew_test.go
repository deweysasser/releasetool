package program

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deweysasser/releasetool/homebrew"
	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockGithub stands up an httptest.Server and swaps the homebrew package's
// GitHub client constructor so Brew.Run talks to the mock. Cleanup restores
// the original on test exit. It also neutralizes the `gh auth token` fallback
// (and clears any ambient GITHUB_TOKEN/GH_TOKEN) so Brew.Run doesn't shell
// out to the real gh CLI during tests — that would be slow, non-hermetic,
// and would leak the developer's real token into package state.
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

	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	prevGhAuth := ghAuthToken
	ghAuthToken = func() (string, error) { return "", assert.AnError }
	t.Cleanup(func() { ghAuthToken = prevGhAuth })

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
	assert.Contains(t, rcBody, "class CumulusAT120Rc1 < Formula")
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

// TestBrew_Run_FetchesReposConcurrently verifies that fetching releases for
// multiple repos runs in parallel (the wall-clock stays near max(rtt), not
// sum(rtt)) and that the per-repo result order is deterministic despite the
// parallelism. The mock server applies a fixed delay to every /releases
// response; if the fetches were serial, the total run would be >= N*delay,
// so we assert the run finishes well below that.
func TestBrew_Run_FetchesReposConcurrently(t *testing.T) {
	const (
		numRepos    = 5
		perRepoWait = 150 * time.Millisecond
	)

	mux := http.NewServeMux()
	newMockGithub(t, mux)

	var maxInFlight, curInFlight int32
	var mu sync.Mutex

	for i := 1; i <= numRepos; i++ {
		repo := fmt.Sprintf("tool%d", i)
		// Metadata handler is fast; the release list carries the delay, which
		// is the dominant cost.
		mux.HandleFunc("/repos/o/"+repo, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{}`)
		})
		mux.HandleFunc("/repos/o/"+repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
			cur := atomic.AddInt32(&curInFlight, 1)
			mu.Lock()
			if cur > maxInFlight {
				maxInFlight = cur
			}
			mu.Unlock()
			time.Sleep(perRepoWait)
			atomic.AddInt32(&curInFlight, -1)

			fmt.Fprintf(w, `[{"tag_name": "v1.0.0", "prerelease": false, "assets": []}]`)
		})
	}

	dir := t.TempDir()
	t.Chdir(dir)

	repoArgs := make([]string, numRepos)
	for i := range repoArgs {
		repoArgs[i] = fmt.Sprintf("o/tool%d", i+1)
	}
	b := &Brew{Repo: repoArgs}

	start := time.Now()
	require.NoError(t, b.Run(&Options{}))
	elapsed := time.Since(start)

	// Parallel-fetch proof: with perRepoWait = 150ms and 5 repos, a serial
	// fetch would take >= 750ms. A parallel fetch should be closer to 150ms
	// plus scheduling overhead. A 2x serial budget (300ms) gives plenty of
	// slack while still catching a regression to serial.
	assert.Less(t, elapsed, time.Duration(numRepos/2)*perRepoWait,
		"fetch phase looks serial: elapsed %s vs per-repo wait %s across %d repos",
		elapsed, perRepoWait, numRepos)

	// Concurrency proof: at least 2 /releases requests were in flight at once.
	assert.Greater(t, int(maxInFlight), 1,
		"no concurrent /releases requests observed; fetches ran one at a time")

	// Every repo got both files written.
	for i := 1; i <= numRepos; i++ {
		for _, suffix := range []string{".rb", "@1.0.0.rb"} {
			name := fmt.Sprintf("tool%d%s", i, suffix)
			_, err := os.Stat(filepath.Join(dir, name))
			assert.NoError(t, err, "missing %s", name)
		}
	}
}

// TestBrew_Run_DedupesAssetDownloadsAcrossDefaultAndNewest proves that when
// the default formula reuses the newest release's assets, the futures cache
// in homebrew/files.go collapses the two recipes' asset sets to a single
// download per unique URL. Without that dedup, we'd hit the asset handler
// 12 times (2 versioned × 4 + 1 default × 4) instead of 8.
func TestBrew_Run_DedupesAssetDownloadsAcrossDefaultAndNewest(t *testing.T) {
	mux := http.NewServeMux()
	server := newMockGithub(t, mux)

	mux.HandleFunc("/repos/o/tool", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})

	assetJSON := func(tag string) string {
		a := func(platform string) string {
			url := fmt.Sprintf("%s/dl/%s/tool-%s-%s.tar.gz", server.URL, tag, tag, platform)
			return fmt.Sprintf(`{"name":"tool-%s-%s.tar.gz","url":"%s","browser_download_url":"%s"}`,
				tag, platform, url, url)
		}
		return fmt.Sprintf(`[%s,%s,%s,%s]`,
			a("darwin-amd64"), a("darwin-arm64"), a("linux-amd64"), a("linux-arm64"))
	}
	mux.HandleFunc("/repos/o/tool/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
			{"tag_name":"v1.0.0","prerelease":false,"assets":%s},
			{"tag_name":"v0.9.0","prerelease":false,"assets":%s}
		]`, assetJSON("v1.0.0"), assetJSON("v0.9.0"))
	})

	var assetHits int32
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&assetHits, 1)
		w.Write([]byte{0})
	})

	dir := t.TempDir()
	t.Chdir(dir)

	b := &Brew{Repo: []string{"o/tool"}}
	require.NoError(t, b.Run(&Options{}))

	assert.Equal(t, int32(8), atomic.LoadInt32(&assetHits),
		"expected 8 asset downloads (2 releases × 4 platforms); got %d — "+
			"default-formula assets must share URLs with newest versioned formula "+
			"so the futures cache dedupes them to one download per URL",
		assetHits)
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
