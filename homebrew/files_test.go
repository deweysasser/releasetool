package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedSha256 returns the hex-encoded sha256 of b, matching how Sha256 computes it.
func expectedSha256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestSha256_PublicFile(t *testing.T) {
	payload := []byte("hello world — a realistic release artifact payload")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/dl/artifact.zip", r.URL.Path)
		w.Write(payload)
	}))
	t.Cleanup(server.Close)

	url := server.URL + "/dl/artifact.zip"
	pf := PackageFile{
		private: false,
		ReleaseAsset: &github.ReleaseAsset{
			BrowserDownloadURL: github.Ptr(url),
			URL:                github.Ptr("unused-for-public"),
		},
	}

	got, err := pf.Sha256()
	require.NoError(t, err)
	assert.Equal(t, expectedSha256(payload), got)
}

func TestSha256_PrivateFileUsesAPIURL(t *testing.T) {
	payload := []byte("private artifact bytes")

	var hitPath, gotAccept, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		w.Write(payload)
	}))
	t.Cleanup(server.Close)

	t.Setenv("GITHUB_TOKEN", "test-token-xyz")

	apiURL := server.URL + "/repos/o/r/releases/assets/42"
	pf := PackageFile{
		private: true,
		ReleaseAsset: &github.ReleaseAsset{
			URL:                github.Ptr(apiURL),
			BrowserDownloadURL: github.Ptr(server.URL + "/should-not-be-used"),
		},
	}

	got, err := pf.Sha256()
	require.NoError(t, err)

	assert.Equal(t, expectedSha256(payload), got)
	assert.Equal(t, "/repos/o/r/releases/assets/42", hitPath,
		"private PackageFile must fetch via the API URL, not the browser URL")
	assert.Equal(t, "application/octet-stream", gotAccept,
		"Accept must be application/octet-stream so the API returns raw bytes")
	assert.Equal(t, "Bearer test-token-xyz", gotAuth,
		"GITHUB_TOKEN must be sent as a bearer token for private asset fetches")
}

func TestSha256_PublicFileSendsNoAuth(t *testing.T) {
	// Public path goes through http.DefaultClient when GITHUB_TOKEN is unset,
	// so no Authorization header should be attached.
	t.Setenv("GITHUB_TOKEN", "")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte("x"))
	}))
	t.Cleanup(server.Close)

	pf := PackageFile{
		private:      false,
		ReleaseAsset: &github.ReleaseAsset{BrowserDownloadURL: github.Ptr(server.URL + "/pub.zip")},
	}

	_, err := pf.Sha256()
	require.NoError(t, err)
	assert.Empty(t, gotAuth, "public Sha256 fetch must not attach an Authorization header")
}

func TestSha256_ErrorOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	t.Cleanup(server.Close)

	pf := PackageFile{
		ReleaseAsset: &github.ReleaseAsset{BrowserDownloadURL: github.Ptr(server.URL + "/gone.zip")},
	}

	_, err := pf.Sha256()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "410")
}

func TestSha256_FutureCachesResult(t *testing.T) {
	payload := []byte("cache me once")
	var hits int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write(payload)
	}))
	t.Cleanup(server.Close)

	pf := PackageFile{
		ReleaseAsset: &github.ReleaseAsset{
			BrowserDownloadURL: github.Ptr(server.URL + "/cached.zip"),
		},
	}

	first, err := pf.Sha256()
	require.NoError(t, err)
	second, err := pf.Sha256()
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, expectedSha256(payload), first)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits),
		"Sha256 must memoize via future() and download the asset only once per PackageFile")
}
