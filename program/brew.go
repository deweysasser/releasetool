package program

import (
	"errors"
	"fmt"
	"github.com/deweysasser/releasetool/homebrew"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"sync/atomic"
)

type Brew struct {
	Version     string   `help:"Version of this release"`
	Description string   `help:"Brew description"`
	ConfigFile  string   `short:"f" type:"existingfile" help:"config file from which to read recipe config"`
	Repo        []string `arg:"" optional:"" help:"Github owner/repo"`
}

type FileList struct {
	Owner   string            `json:"owner"`
	Recipes []homebrew.Recipe `json:"recipes"`
}

func (b *Brew) AfterApply() error {
	if b.ConfigFile == "" && len(b.Repo) < 1 {
		return errors.New("one of --config-file or repo argument must be specified")
	}
	return nil
}

func (b *Brew) Run(options *Options) error {

	_ = options

	var generatedFileCount int64

	var recipes []homebrew.Recipe

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
			r.Normalize()
			if r.Owner == "" {
				r.Owner = list.Owner
			}

			recipes = append(recipes, r)
		}
	}

	for _, repo := range b.Repo {

		r := homebrew.Recipe{Repo: repo}

		r.Normalize()
		if r.Owner == "" {
			return fmt.Errorf("repo %s must have format owner/repo", repo)
		}

		recipes = append(recipes, r)
	}

	err := parallel[homebrew.Recipe](recipes, func(r homebrew.Recipe) error {

		generated, err := b.HandleRecipe(r)
		if generated {
			atomic.AddInt64(&generatedFileCount, 1)
		}
		return err
	})

	if err != nil {
		return err
	}

	log.Debug().Int64("generatedFileCount", generatedFileCount).Msg("Generated files")

	if generatedFileCount > 0 {
		err = homebrew.WriteLibFile()
		if err != nil {
			return err
		}
	}

	return err
}

func (b *Brew) HandleRecipe(r homebrew.Recipe) (bool, error) {
	log := log.With().
		Str("owner", r.Owner).
		Str("repo", r.Repo).
		Str("desc", r.Description).
		Logger()

	log.Debug().Msg("Handling recipe")

	out := r.Repo + ".rb"

	err := r.FillFromGithub()
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(out); err == nil {
		log.Debug().Msg("Existing output file")
		current, err := homebrew.ParseRecipeFile(out)
		if err != nil {
			return false, err
		}

		if current.Version == r.Version {
			log.Debug().Msg("Version match. Nothing to do")
			return false, nil
		} else {
			log.Debug().
				Str("current_version", current.Version).
				Str("github_version", r.Version).
				Msg("Different version on github")
		}
	}

	for _, f := range r.Files {
		// We don't do windows right now, so there's no point in calculating the hash
		if !strings.Contains(f.String(), "-windows-") {
			go f.Sha256() // pre-warm the calculation
		}
	}

	tmp := out + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return false, err
	}

	defer f.Close()

	err = r.Generate(f)

	if err != nil {

		return false, err
	}

	// The file is written, now rename it to the final target
	err = f.Close()
	if err != nil {
		return false, err
	}

	err = os.Rename(tmp, out)
	if err != nil {
		return false, err
	}

	return true, nil
}
