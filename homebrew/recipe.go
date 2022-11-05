package homebrew

import (
	"context"
	_ "embed"
	"errors"
	"github.com/google/go-github/v48/github"
	"github.com/rs/zerolog/log"
	"io"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed template.rb
var recipe string

type Recipe struct {
	Owner       string        `json:"owner" yaml:"owner"`
	Repo        string        `json:"repo" yaml:"repo"`
	Version     string        `json:"-" yaml:"-"`
	Description string        `json:"description" yaml:"description"`
	Files       []PackageFile `json:"-"`
}

func (b *Recipe) FillFromGithub() error {
	client := github.NewClient(nil)

	if b.Description == "" {
		repo, _, err := client.Repositories.Get(context.Background(), b.Owner, b.Repo)
		if err != nil {
			return err
		}

		b.Description = repo.GetDescription()
	}

	releases, _, err := client.Repositories.ListReleases(context.Background(), b.Owner, b.Repo, &github.ListOptions{})

	if err != nil {
		return err
	}

	if len(releases) < 1 {
		return errors.New("no release found")
	}

	release := releases[0]

	log.Debug().Str("tag", release.GetTagName()).Msg("Found release")

	// TODO:  if the recipe specifies a release, find it and use that instead
	b.Version = release.GetTagName()
	for _, asset := range release.Assets {
		log.Debug().
			Str("name", asset.GetName()).
			Str("url", asset.GetBrowserDownloadURL()).
			Msg("Found asset")
		b.Files = append(b.Files, PackageFile(asset.GetBrowserDownloadURL()))
	}

	return nil
}

func filterFiles(b *Recipe, terms ...string) []PackageFile {
	results := make([]PackageFile, 0)

top:
	for _, f := range b.Files {
		for _, term := range terms {
			if !strings.Contains(string(f), term) {
				continue top
			}
		}

		results = append(results, f)
	}

	return results
}

var recipeTemplate *template.Template

func init() {

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
		panic(err)
	}

	recipeTemplate = temp
}

func (r *Recipe) Generate(output io.Writer) error {

	log.Debug().Str("name", r.Repo).Msg("Generating recipe")

	if len(r.Files) == 0 {
		log.Debug().Msg("No files given -- fetching from github")
		if err := r.FillFromGithub(); err != nil {
			return err
		}
	}

	return recipeTemplate.Execute(output, r)
}

func titleCase(s string) string {
	return strings.ToTitle(s[:1]) + strings.ToLower(s[1:])
}
