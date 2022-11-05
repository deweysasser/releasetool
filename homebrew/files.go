package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type PackageFile string

func (p PackageFile) Basename() string {
	return filepath.Base(string(p))
}

func (p PackageFile) Sum() (string, error) {
	var input io.ReadCloser

	if strings.HasPrefix(string(p), "http") {
		resp, err := http.Get(string(p))
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
	var debugOut io.WriteCloser

	if zerolog.GlobalLevel() == zerolog.DebugLevel && p.Basename() != string(p) {
		log.Debug().Str("output_file", p.Basename()).Msg("saving downloaded asset")
		d, err := os.Create(p.Basename())
		if err != nil {
			return "", err
		}
		debugOut = d
		defer d.Close()
	}

	sha := sha256.New()
	bytes := make([]byte, 32*1024*1024)

	for {
		n, e := input.Read(bytes)
		sha.Write(bytes[:n])
		if debugOut != nil {
			debugOut.Write(bytes[:n])
		}
		if e != nil {
			break
		}
	}
	return hex.EncodeToString(sha.Sum(nil)), nil
}
