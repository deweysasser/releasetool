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
