package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/go-github/v48/github"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"path/filepath"
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

var futures = make(map[PackageFile]func() (string, error))

func (p PackageFile) Sha256() (string, error) {

	if _, ok := futures[p]; !ok {
		futures[p] = future(func() (string, error) {

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
	}

	return futures[p]()
}
