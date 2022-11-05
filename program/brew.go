package program

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"github.com/google/go-github/v48/github"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed template.rb
var recipe string

type PackageFile string

type Brew struct {
	Owner       string        `help:"Github owner"`
	Repo        string        `help:"Github repo"`
	Version     string        `help:"Version of this release"`
	Description string        `help:"Brew description"`
	File        []PackageFile `arg:"" optional:""`
}

func (b *Brew) Run(options *Options) error {

	if len(b.File) == 0 {
		log.Debug().Msg("No files given -- fetching from github")
		if err := b.FillFromGithub(); err != nil {
			return err
		}
	}

	temp, err := template.New("recipe").
		Funcs(map[string]any{
			"files":    filterFiles,
			"title":    titleCase,
			"upper":    strings.ToUpper,
			"lower":    strings.ToLower,
			"basename": filepath.Base,
		}).
		Parse(recipe)
	if err != nil {
		return err
	}

	return temp.Execute(os.Stdout, b)
}

func titleCase(s string) string {
	return strings.ToTitle(s[:1]) + strings.ToLower(s[1:])
}

func (b *Brew) FillFromGithub() error {
	client := github.NewClient(nil)

	releases, _, err := client.Repositories.ListReleases(context.Background(), b.Owner, b.Repo, &github.ListOptions{})

	if err != nil {
		return err
	}

	if len(releases) < 1 {
		return errors.New("No release found")
	}

	release := releases[0]

	log.Debug().Str("tag", release.GetTagName()).Msg("Found release")

	b.Version = release.GetTagName()
	for _, asset := range release.Assets {
		log.Debug().
			Str("name", asset.GetName()).
			Str("url", asset.GetBrowserDownloadURL()).
			Msg("Found asset")
		b.File = append(b.File, PackageFile(asset.GetBrowserDownloadURL()))
	}

	return nil
}

func filterFiles(b *Brew, terms ...string) []PackageFile {
	results := make([]PackageFile, 0)

top:
	for _, f := range b.File {
		for _, term := range terms {
			if !strings.Contains(string(f), term) {
				continue top
			}
		}

		results = append(results, f)
	}

	return results
}

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
		log.Debug().Int("read", n).Int("size", len(bytes[:n])).Msg("reading & writing")
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

func (p PackageFile) isMacOS() bool {
	return strings.Contains(string(p), "darwin")
}

func (p PackageFile) isLinux() bool {
	return strings.Contains(string(p), "linux")
}

func (p PackageFile) isAMD64() bool {
	return strings.Contains(string(p), "amd64")
}

func (p PackageFile) isARM64() bool {
	return strings.Contains(string(p), "arm64")
}
