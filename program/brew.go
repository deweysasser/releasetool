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
	Owner   string            `json:"owner"`
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

		return parallel[homebrew.Recipe](list.Recipes, func(r homebrew.Recipe) error {
			r.Normalize()
			if r.Owner == "" {
				r.Owner = list.Owner
			}

			return b.HandleRecipe(r)
		})

		return err
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
	log := log.With().
		Str("owner", r.Owner).
		Str("repo", r.Repo).
		Str("desc", r.Description).
		Logger()

	log.Debug().Msg("Handling recipe")

	out := r.Repo + ".rb"

	err := r.FillFromGithub()
	if err != nil {
		return err
	}

	if _, err := os.Stat(out); err == nil {
		log.Debug().Msg("Existing output file")
		current, err := homebrew.ParseRecipeFile(out)
		if err != nil {
			return err
		}

		if current.Version == r.Version {
			log.Debug().Msg("Version match. Nothing to do")
			return nil
		} else {
			log.Debug().
				Str("current_version", current.Version).
				Str("github_version", r.Version).
				Msg("Different version on github")
		}
	}

	for _, f := range r.Files {
		go f.Sum() // pre-warm the calculation
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}

	defer f.Close()

	return r.Generate(f)
}
