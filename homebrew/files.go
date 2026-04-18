package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/deweysasser/releasetool/timing"
	"github.com/google/go-github/v84/github"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

type PackageFile struct {
	private bool
	*github.ReleaseAsset
}

func (p PackageFile) String() string {
	return p.Basename()
}

func (p *PackageFile) Basename() string {
	return filepath.Base(p.GetBrowserDownloadURL())
}

func (p PackageFile) URL() string {
	return p.GetURL()
}

var (
	futuresMu sync.Mutex
	futures   = make(map[PackageFile]func() (string, error))
)

func (p PackageFile) Sha256() (string, error) {
	// Fast path: since 2025-06-24 GitHub publishes a sha256 digest as
	// metadata on every new release asset, exposed as `digest` in the
	// REST/GraphQL asset objects. Reading it here skips the potentially
	// multi-hundred-MB download we'd otherwise do just to hash the bytes.
	// https://github.blog/changelog/2025-06-24-release-asset-sha256-digests-in-rest-and-graphql-apis/
	//
	// SECURITY: the digest is GitHub's metadata, not a hash we recomputed
	// over the bytes the end user will eventually install. If GitHub's
	// release infrastructure were compromised and served bytes that did
	// not match the advertised digest, the formula we generate would
	// record the wrong number — but `brew install` re-hashes the
	// downloaded tarball locally before unpacking and aborts on any
	// mismatch, so the worst outcome is a user-visible install failure,
	// not silent execution of attacker bytes. This is the same trust
	// boundary Homebrew already relies on for every other formula in the
	// ecosystem, so trading a download for a metadata read does not
	// widen the attack surface.
	//
	// Older assets (uploaded before the 2025-06-24 rollout) and assets
	// GitHub chose not to digest return an empty string, so we fall
	// through to the download path below.
	if sum, ok := sha256FromDigest(p.GetDigest()); ok {
		log.Debug().
			Str("file", p.String()).
			Str("SHA256", sum).
			Msg("using sha256 from GitHub release-asset digest (no download)")
		return sum, nil
	}

	futuresMu.Lock()
	f, ok := futures[p]
	if !ok {
		f = future(func() (string, error) {
			// Timed inside the future so cache hits don't log a bogus download.
			defer timing.Start("sha256 " + p.String()).Done()

			log.Debug().Str("file", p.String()).Msg("finding sha256")

			var input io.ReadCloser

			var url string
			if p.private {
				url = p.GetURL()
			} else {
				url = p.GetBrowserDownloadURL()
			}

			client := githubHttpClient()

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return "", err
			}

			req.Header.Set("Accept", "application/octet-stream")

			resp, err := client.Do(req)

			if err != nil {
				return "", err
			}

			if resp.StatusCode != 200 {
				return "", fmt.Errorf("error %d: failed to download file %s via %s", resp.StatusCode, p.String(), url)
			}

			input = resp.Body

			defer input.Close()

			sha := sha256.New()
			bytes := make([]byte, 32*1024*1024)

			l := 0
			for {
				n, e := input.Read(bytes)
				outbytes := bytes[:n]
				l += len(outbytes)
				sha.Write(outbytes)

				if e != nil {
					break
				}
			}

			sumStr := hex.EncodeToString(sha.Sum(nil))

			log.Debug().
				Str("file", p.String()).
				Str("SHA256", sumStr).
				Int("size", l).
				Msg("Downloaded file")
			return sumStr, nil
		})
		futures[p] = f
	}
	futuresMu.Unlock()

	return f()
}

// sha256FromDigest extracts the hex sha256 from GitHub's release-asset
// digest field (format "sha256:<hex>"). Returns ok=false when the digest
// is empty (historical/undigested asset), uses a non-sha256 algorithm
// (forward-compat with any future digest scheme GitHub adds), or is not
// a well-formed 64-char hex string.
func sha256FromDigest(d string) (string, bool) {
	const prefix = "sha256:"
	if !strings.HasPrefix(d, prefix) {
		return "", false
	}
	h := strings.ToLower(d[len(prefix):])
	if len(h) != sha256.Size*2 {
		return "", false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", false
		}
	}
	return h, true
}
