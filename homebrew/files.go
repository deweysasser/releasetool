package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/rs/zerolog/log"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type PackageFile string

func (p PackageFile) Basename() string {
	return filepath.Base(string(p))
}

var futures = make(map[PackageFile]func() (string, error))

func (p PackageFile) Sum() (string, error) {

	if _, ok := futures[p]; !ok {
		futures[p] = future(func() (string, error) {

			log.Debug().Str("file", string(p)).Msg("finding sha256")

			var input io.ReadCloser

			if strings.HasPrefix(string(p), "http") {
				client := githubHttpClient()
				resp, err := client.Get(string(p))
				if err != nil {
					return "", err
				}

				input = resp.Body
			} else {
				f, err := os.Open(string(p))
				if err != nil {
					return "", err
				}
				input = f
			}

			defer input.Close()

			sha := sha256.New()
			bytes := make([]byte, 32*1024*1024)

			for {
				n, e := input.Read(bytes)
				sha.Write(bytes[:n])

				if e != nil {
					break
				}
			}
			return hex.EncodeToString(sha.Sum(nil)), nil
		})
	}

	return futures[p]()
}
