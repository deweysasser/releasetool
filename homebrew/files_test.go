package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestSha256_ConcurrentDifferentFiles exercises the package-level futures
// cache from many goroutines at once with distinct PackageFile keys. This is
// the access pattern HandleRecipe uses when pre-warming hashes, so it must be
// safe under -race.
func TestSha256_ConcurrentDifferentFiles(t *testing.T) {
	const n = 32
	payloads := make([][]byte, n)
	for i := range payloads {
		payloads[i] = []byte(fmt.Sprintf("artifact-payload-%d", i))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var idx int
		if _, err := fmt.Sscanf(r.URL.Path, "/dl/%d.zip", &idx); err != nil || idx < 0 || idx >= n {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		w.Write(payloads[idx])
	}))
	t.Cleanup(server.Close)

	files := make([]PackageFile, n)
	for i := range files {
		files[i] = PackageFile{
			ReleaseAsset: &github.ReleaseAsset{
				BrowserDownloadURL: github.Ptr(fmt.Sprintf("%s/dl/%d.zip", server.URL, i)),
			},
		}
	}

	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := range files {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = files[i].Sha256()
		}(i)
	}
	wg.Wait()

	for i := range files {
		require.NoError(t, errs[i], "goroutine %d", i)
		assert.Equal(t, expectedSha256(payloads[i]), results[i], "hash mismatch for file %d", i)
	}
}

// TestSha256_UsesAssetDigest verifies the fast path: when GitHub has
// populated the asset's `digest` field, Sha256 must return the hash from
// metadata and must not issue any HTTP request.
func TestSha256_UsesAssetDigest(t *testing.T) {
	payload := []byte("bytes that MUST NOT be downloaded")
	want := expectedSha256(payload)

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write(payload)
	}))
	t.Cleanup(server.Close)

	pf := PackageFile{
		ReleaseAsset: &github.ReleaseAsset{
			BrowserDownloadURL: github.Ptr(server.URL + "/should-not-be-fetched.zip"),
			Digest:             github.Ptr("sha256:" + want),
		},
	}

	got, err := pf.Sha256()
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, int32(0), atomic.LoadInt32(&hits),
		"fast path must not download the asset when GitHub publishes a sha256 digest")
}

// TestSha256_FallsBackWhenDigestUnusable confirms the download path still
// runs for assets without a usable digest — historical pre-2025-06 assets
// return an empty Digest, and we must not claim an unknown hash is valid.
func TestSha256_FallsBackWhenDigestUnusable(t *testing.T) {
	payload := []byte("real download fallback")

	cases := []struct {
		name, digest string
	}{
		{"empty", ""},
		{"wrong_algorithm", "sha512:" + strings.Repeat("a", 128)},
		{"missing_prefix", strings.Repeat("a", 64)},
		{"wrong_length", "sha256:deadbeef"},
		{"non_hex_char", "sha256:" + strings.Repeat("z", 64)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(payload)
			}))
			t.Cleanup(server.Close)

			asset := &github.ReleaseAsset{
				BrowserDownloadURL: github.Ptr(server.URL + "/fallback-" + tc.name + ".zip"),
			}
			if tc.digest != "" {
				asset.Digest = github.Ptr(tc.digest)
			}
			pf := PackageFile{ReleaseAsset: asset}

			got, err := pf.Sha256()
			require.NoError(t, err)
			assert.Equal(t, expectedSha256(payload), got,
				"fallback download must compute the real sha256 when the digest is %s", tc.name)
		})
	}
}

func TestSha256FromDigest(t *testing.T) {
	validHex := strings.Repeat("a", 64)
	mixedHex := "DEADBEEF" + strings.Repeat("a", 56)

	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"valid_lowercase", "sha256:" + validHex, validHex, true},
		{"valid_mixed_case_normalized", "sha256:" + mixedHex, strings.ToLower(mixedHex), true},
		{"empty", "", "", false},
		{"wrong_algorithm", "sha512:" + strings.Repeat("a", 128), "", false},
		{"missing_colon", "sha256" + validHex, "", false},
		{"short_hex", "sha256:deadbeef", "", false},
		{"non_hex", "sha256:" + strings.Repeat("g", 64), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sha256FromDigest(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
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
