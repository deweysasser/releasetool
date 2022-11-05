package program

import (
	"errors"
	"github.com/deweysasser/releasetool/homebrew"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
)

type Brew struct {
	Version     string                 `help:"Version of this release"`
	Description string                 `help:"Brew description"`
	ConfigFile  string                 `short:"f" type:"existingfile" help:"config file from which to read recipe config"`
	Repo        string                 `arg:"" optional:"" help:"Github owner/repo"`
	File        []homebrew.PackageFile `arg:"" optional:""`
}

type FileList struct {
	Recipes []homebrew.Recipe `json:"recipes"`
}

func (b *Brew) AfterApply() error {
	if b.ConfigFile == "" && b.Repo == "" {
		return errors.New("one of --config-file or repo argument must be specified")
	}
	return nil
}

func (b *Brew) Run(options *Options) error {

	_ = options

	if b.ConfigFile != "" {
		log.Debug().Msg("Have config file")
		list := &FileList{}
		bytes, err := os.ReadFile(b.ConfigFile)
		if err != nil {
			return err
		}
		err = yaml.Unmarshal(bytes, list)
		if err != nil {
			return err
		}

		for _, r := range list.Recipes {
			err = b.HandleRecipe(r)
			if err != nil {
				return err
			}
		}

		return nil
	}

	parts := strings.Split(b.Repo, "/")
	if len(parts) != 2 {
		return errors.New("repo must be in format owner/repo: " + b.Repo)
	}

	owner, repo := parts[0], parts[1]

	r := homebrew.Recipe{
		Owner:       owner,
		Repo:        repo,
		Version:     b.Version,
		Description: b.Description,
		Files:       b.File,
	}

	return r.Generate(os.Stdout)
}

func (b *Brew) HandleRecipe(r homebrew.Recipe) error {
	log.Debug().
		Str("name", r.Repo).
		Str("desc", r.Description).
		Msg("Handling recipe")
	out := r.Repo + ".rb"

	f, err := os.Create(out)
	if err != nil {
		return err
	}

	defer f.Close()

	return r.Generate(f)
}
