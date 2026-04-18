package program

import (
	"errors"
	"fmt"
	"github.com/deweysasser/releasetool/homebrew"
	"github.com/rs/zerolog/log"
	"os"
	"strings"
	"sync/atomic"
)

// fetchJob is the work unit for the parallel fetch+expand phase. `out` points
// at the caller's pre-allocated result slot so the goroutine can write without
// a shared mutex — slot-per-goroutine keeps output order deterministic.
type fetchJob struct {
	base *homebrew.Recipe
	out  *[]*homebrew.Recipe
}

type Brew struct {
	Version     string   `help:"Version of this release"`
	Description string   `help:"Brew description"`
	ConfigFile  string   `short:"f" type:"existingfile" help:"config file from which to read recipe config"`
	Repo        []string `arg:"" optional:"" help:"Github owner/repo"`
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

	var recipes []*homebrew.Recipe

	var configFile *ConfigFile

	if b.ConfigFile != "" {
		log.Debug().Msg("Have config file")
		cf, err := NewConfigFile(b)
		if err != nil {
			return err
		}

		configFile = cf

		for _, r := range cf.Recipes {
			r.Normalize()
			if r.Owner == "" {
				r.Owner = cf.Owner
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

		recipes = append(recipes, &r)
	}

	// Expand each configured recipe into (N versioned + optional default)
	// output recipes — one .rb file per release plus the default unversioned
	// formula pointing at the newest non-prerelease. Fetching is the slow part
	// (one Get + paginated ListReleases per repo), so run it concurrently
	// across all base recipes. Each goroutine writes its expansion into its
	// own pre-allocated slot in subsPerBase so the combined output order is
	// deterministic (matches config-file order) — important for the docs step.
	subsPerBase := make([][]*homebrew.Recipe, len(recipes))
	jobs := make([]fetchJob, len(recipes))
	for i, base := range recipes {
		base.Normalize()
		jobs[i] = fetchJob{base: base, out: &subsPerBase[i]}
	}

	if err := parallel[fetchJob](jobs, func(j fetchJob) error {
		releases, err := j.base.FetchReleases()
		if err != nil {
			return err
		}
		subs := homebrew.ExpandVersions(j.base, releases)
		if len(subs) == 0 {
			return fmt.Errorf("no release found for %s/%s", j.base.Owner, j.base.Repo)
		}
		*j.out = subs
		return nil
	}); err != nil {
		return err
	}

	var expanded []*homebrew.Recipe
	var defaults []*homebrew.Recipe
	for _, subs := range subsPerBase {
		expanded = append(expanded, subs...)
		for _, s := range subs {
			if !strings.Contains(s.OutputFile, "@") {
				defaults = append(defaults, s)
			}
		}
	}

	err := parallel[*homebrew.Recipe](expanded, func(r *homebrew.Recipe) error {

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

	if configFile != nil {
		for _, f := range configFile.Docs {
			if err := f.Update(configFile, defaults); err != nil {
				return err
			}
		}
	}

	return err
}

func (b *Brew) HandleRecipe(r *homebrew.Recipe) (bool, error) {
	log := log.With().
		Str("owner", r.Owner).
		Str("repo", r.Repo).
		Str("desc", r.Description).
		Str("version", r.Version).
		Str("output", r.OutputFile).
		Logger()

	log.Debug().Msg("Handling recipe")

	out := r.OutputFile
	versioned := strings.Contains(out, "@")

	if _, err := os.Stat(out); err == nil {
		if versioned {
			// Versioned formulas are immutable once written — a given
			// (repo, version) always produces the same content.
			log.Debug().Msg("Versioned formula exists; skipping")
			return false, nil
		}
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
