package homebrew

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGithub stands up an httptest.Server, swaps newGithubClient to point at it,
// and registers cleanup for both. It returns the server so tests can read its URL.
func mockGithub(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	orig := newGithubClient
	newGithubClient = func() *github.Client {
		c := github.NewClient(nil)
		u, err := url.Parse(server.URL + "/")
		require.NoError(t, err)
		c.BaseURL = u
		c.UploadURL = u
		return c
	}
	t.Cleanup(func() { newGithubClient = orig })

	return server
}

func TestFillFromGithub_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	server := mockGithub(t, mux)

	mux.HandleFunc("/repos/deweysasser/cumulus", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		fmt.Fprintf(w, `{
			"name": "cumulus",
			"description": "A better AWS CLI",
			"private": false
		}`)
	})

	mux.HandleFunc("/repos/deweysasser/cumulus/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{
			"tag_name": "v0.5.0",
			"assets": [
				{
					"name": "cumulus-darwin-amd64.zip",
					"url": "%[1]s/repos/deweysasser/cumulus/releases/assets/1",
					"browser_download_url": "%[1]s/download/v0.5.0/cumulus-darwin-amd64.zip",
					"size": 1234
				},
				{
					"name": "cumulus-linux-amd64.zip",
					"url": "%[1]s/repos/deweysasser/cumulus/releases/assets/2",
					"browser_download_url": "%[1]s/download/v0.5.0/cumulus-linux-amd64.zip",
					"size": 5678
				}
			]
		}]`, server.URL)
	})

	r := &Recipe{Owner: "deweysasser", Repo: "cumulus"}
	require.NoError(t, r.FillFromGithub())

	assert.Equal(t, "A better AWS CLI", r.Description)
	assert.Equal(t, "v0.5.0", r.Version)
	assert.False(t, r.PrivateRepo)
	require.Len(t, r.Files, 2)
	assert.Equal(t, "cumulus-darwin-amd64.zip", r.Files[0].GetName())
	assert.Equal(t, "cumulus-linux-amd64.zip", r.Files[1].GetName())
}

func TestFillFromGithub_PreservesExistingDescription(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"description": "from github"}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"tag_name": "v1.0.0", "assets": []}]`)
	})

	r := &Recipe{Owner: "o", Repo: "r", Description: "user-supplied"}
	require.NoError(t, r.FillFromGithub())
	assert.Equal(t, "user-supplied", r.Description,
		"Description set by the user must not be overwritten by the GitHub repo description")
}

func TestFillFromGithub_PrivateRepoFlagPropagates(t *testing.T) {
	mux := http.NewServeMux()
	server := mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"description": "d", "private": true}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{
			"tag_name": "v1.0.0",
			"assets": [{"name": "a.zip", "url": "%[1]s/asset/1", "browser_download_url": "%[1]s/dl/a.zip"}]
		}]`, server.URL)
	})

	r := &Recipe{Owner: "o", Repo: "r"}
	require.NoError(t, r.FillFromGithub())

	assert.True(t, r.PrivateRepo)
	require.Len(t, r.Files, 1)
	assert.True(t, r.Files[0].private,
		"PackageFile.private must be set when the repo is private (drives Sha256 URL choice)")
}

func TestFillFromGithub_NoReleases(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[]`)
	})

	r := &Recipe{Owner: "o", Repo: "r"}
	err := r.FillFromGithub()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no release found")
}

func TestFillFromGithub_RepoNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Not Found"}`, http.StatusNotFound)
	})

	r := &Recipe{Owner: "o", Repo: "r"}
	err := r.FillFromGithub()
	assert.Error(t, err)
}

func TestFetchReleases_PaginatesAllPages(t *testing.T) {
	mux := http.NewServeMux()
	server := mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			// First page signals a next page via the Link header; this is how
			// go-github populates resp.NextPage.
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/releases?page=2>; rel="next"`, server.URL))
			fmt.Fprint(w, `[
				{"tag_name": "v1.2.0", "assets": [{"name": "tool-linux-amd64.zip", "url": "u1", "browser_download_url": "d1"}]},
				{"tag_name": "v1.1.0", "assets": []}
			]`)
		case "2":
			fmt.Fprint(w, `[{"tag_name": "v1.0.0", "assets": []}]`)
		default:
			t.Fatalf("unexpected page %q", page)
		}
	})

	rec := &Recipe{Owner: "o", Repo: "r"}
	releases, err := rec.FetchReleases()
	require.NoError(t, err)
	require.Len(t, releases, 3, "all pages must be collected")
	assert.Equal(t, "v1.2.0", releases[0].Version)
	assert.Equal(t, "v1.1.0", releases[1].Version)
	assert.Equal(t, "v1.0.0", releases[2].Version)
	// Assets attach to the right release.
	require.Len(t, releases[0].Files, 1)
	assert.Equal(t, "tool-linux-amd64.zip", releases[0].Files[0].GetName())
}

func TestFetchReleases_ReturnsPrereleaseFlag(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name": "v2.0.0", "prerelease": false, "assets": []},
			{"tag_name": "v1.5.0", "prerelease": true,  "assets": []}
		]`)
	})

	rec := &Recipe{Owner: "o", Repo: "r"}
	releases, err := rec.FetchReleases()
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.False(t, releases[0].Prerelease, "stable release must propagate prerelease=false")
	assert.True(t, releases[1].Prerelease, "prerelease flag from GitHub must propagate")
}

func TestFetchReleases_InfersPrereleaseFromRCTag(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		// Repo forgot to set the prerelease flag, but the tags are clearly
		// prereleases by convention. Covers both the bare "-rc1" form and
		// the semver "-rc.1" / "-alpha.1" dotted-identifier forms.
		fmt.Fprint(w, `[
			{"tag_name": "v1.0.0-rc1",    "prerelease": false, "assets": []},
			{"tag_name": "v1.0.0-rc.1",   "prerelease": false, "assets": []},
			{"tag_name": "v1.0.0-alpha.1","prerelease": false, "assets": []}
		]`)
	})

	rec := &Recipe{Owner: "o", Repo: "r"}
	releases, err := rec.FetchReleases()
	require.NoError(t, err)
	require.Len(t, releases, 3)
	for _, rel := range releases {
		assert.True(t, rel.Prerelease,
			"tag %q must be detected as a prerelease via suffix convention", rel.Version)
	}
}

// TestFetchReleases_SemverDottedRCPropagatesEndToEnd proves a semver-style
// dotted prerelease tag survives the full FetchReleases -> ExpandVersions
// pipeline: it lands in the right output filename and class name, and it
// does not become the default formula.
func TestFetchReleases_SemverDottedRCPropagatesEndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/tool", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/tool/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name": "v1.2.0-rc.1", "prerelease": true,  "assets": []},
			{"tag_name": "v1.1.0",      "prerelease": false, "assets": []}
		]`)
	})

	base := &Recipe{Owner: "o", Repo: "tool"}
	releases, err := base.FetchReleases()
	require.NoError(t, err)

	recipes := ExpandVersions(base, releases)
	require.Len(t, recipes, 3, "2 versioned + 1 default")

	// Find the rc entry by version and assert it maps to the semver-dotted
	// filename while the class name collapses the dot (tokenWordsOnly).
	var rc *Recipe
	for _, r := range recipes {
		if r.Version == "v1.2.0-rc.1" {
			rc = r
		}
	}
	require.NotNil(t, rc, "expected a recipe for v1.2.0-rc.1")
	assert.Equal(t, "tool@1.2.0-rc.1.rb", rc.OutputFile,
		"filename must preserve the semver dot so `brew install tool@1.2.0-rc.1` works")
	assert.Equal(t, "ToolAT120rc1", rc.ClassName,
		"class name strips the dot (Ruby identifier constraint) but remains unique per version")
	assert.True(t, rc.Prerelease)

	// Default points at the stable release, not the rc.
	var def *Recipe
	for _, r := range recipes {
		if r.OutputFile == "tool.rb" {
			def = r
		}
	}
	require.NotNil(t, def, "a default unversioned recipe must exist for the stable release")
	assert.Equal(t, "v1.1.0", def.Version)
}

func TestFetchReleases_SurfacesAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server exploded", http.StatusInternalServerError)
	})

	rec := &Recipe{Owner: "o", Repo: "r"}
	_, err := rec.FetchReleases()
	require.Error(t, err)
}

func TestFillFromGithub_PrefersNewestStableOverPrerelease(t *testing.T) {
	mux := http.NewServeMux()
	server := mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
			{"tag_name": "v1.2.0-rc1", "prerelease": true, "assets": [
				{"name": "tool-rc.zip", "url": "%[1]s/asset/rc", "browser_download_url": "%[1]s/dl/rc"}
			]},
			{"tag_name": "v1.1.0", "prerelease": false, "assets": [
				{"name": "tool-stable.zip", "url": "%[1]s/asset/s", "browser_download_url": "%[1]s/dl/s"}
			]}
		]`, server.URL)
	})

	rec := &Recipe{Owner: "o", Repo: "r"}
	require.NoError(t, rec.FillFromGithub())
	assert.Equal(t, "v1.1.0", rec.Version,
		"FillFromGithub must skip the prerelease and pick the newest stable")
	require.Len(t, rec.Files, 1)
	assert.Equal(t, "tool-stable.zip", rec.Files[0].GetName())
}

func TestFillFromGithub_FallsBackToNewestWhenOnlyPrereleases(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name": "v1.0.0-rc2", "prerelease": true, "assets": []},
			{"tag_name": "v1.0.0-rc1", "prerelease": true, "assets": []}
		]`)
	})

	rec := &Recipe{Owner: "o", Repo: "r"}
	require.NoError(t, rec.FillFromGithub())
	assert.Equal(t, "v1.0.0-rc2", rec.Version,
		"with no stable release, fall back to the newest prerelease so we still generate something")
}

func TestFillFromGithub_ReleaseListError(t *testing.T) {
	mux := http.NewServeMux()
	mockGithub(t, mux)

	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{}`)
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `server exploded`, http.StatusInternalServerError)
	})

	r := &Recipe{Owner: "o", Repo: "r"}
	err := r.FillFromGithub()
	assert.Error(t, err)
}
