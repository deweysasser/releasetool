package homebrew

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"github.com/google/go-github/v48/github"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	PrivateRepo bool          `json:"-"`
	Files       []PackageFile `json:"-"`
}

func NewRecipe(repo, owner, version, description string) (*Recipe, error) {
	if owner == "" {
		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			return nil, errors.New("owner must be specified, or repo must be format owner/repo")
		}

		owner = parts[0]
		repo = parts[1]
	}

	return &Recipe{
		Owner:       owner,
		Repo:        repo,
		Version:     version,
		Description: description,
		Files:       []PackageFile{},
	}, nil
}

// Normalize normalizes a recipe into separate owner and repo if it has an owner/repo string in the repo
func (r *Recipe) Normalize() {
	parts := strings.Split(r.Repo, "/")
	if len(parts) == 2 {
		r.Owner = parts[0]
		r.Repo = parts[1]
	}
}

func (b *Recipe) FillFromGithub() error {
	httpClient := githubHttpClient()

	client := github.NewClient(httpClient)

	repo, _, err := client.Repositories.Get(context.Background(), b.Owner, b.Repo)
	if err != nil {
		return err
	}

	if b.Description == "" {
		b.Description = repo.GetDescription()
	}
	
	b.PrivateRepo = repo.GetPrivate()

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
			Str("browser_url", asset.GetBrowserDownloadURL()).
			Str("assert_url", asset.GetURL()).
			Int("size", asset.GetSize()).
			Msg("Found asset")
		b.Files = append(b.Files, PackageFile{b.PrivateRepo, asset})
	}
	return nil
}

func githubHttpClient() *http.Client {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		log.Debug().
			Str("variable", "GITHUB_TOKEN").
			Msg("Using value of environment variable as oauth2 token")
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		return oauth2.NewClient(ctx, ts)
	}

	return http.DefaultClient
}

func filterFiles(b *Recipe, terms ...string) []PackageFile {
	results := make([]PackageFile, 0)

top:
	for _, f := range b.Files {
		for _, term := range terms {
			if !strings.Contains(f.String(), term) {
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
			"files":     filterFiles,
			"token":     tokenWordsOnly,
			"title":     titleCase,
			"camelcase": camelCase,
			"upper":     strings.ToUpper,
			"lower":     strings.ToLower,
			"basename":  filepath.Base,
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

// These two must be complimentary sets
var allowedLetters = regexp.MustCompile("[a-zA-Z0-9_]+")
var disallowedLetters = regexp.MustCompile("[^a-zA-Z0-9_]")

func tokenWordsOnly(s string) string {
	return disallowedLetters.ReplaceAllString(s, "")
}

func camelCase(s string) string {
	parts := disallowedLetters.Split(s, -1)
	for n, p := range parts {
		if p != "" {
			parts[n] = titleCase(p)
		}
	}

	return strings.Join(parts, "")
}

func titleCase(s string) string {
	return strings.ToTitle(s[:1]) + s[1:]
}

var (
	classline   = regexp.MustCompile("^class ([a-zA-Z0-9_]*)")
	descline    = regexp.MustCompile("^[ \t]*desc \"(.*)\"")
	versionline = regexp.MustCompile("^[ \t]*version \"(v?[0-9\\.]*)\"")
	fileline    = regexp.MustCompile("^[ \t]*url \"(.*)\"")
)

// ParseRecipeFile reads a ruby recipe file and fills out the recipe information
func ParseRecipeFile(file string) (*Recipe, error) {
	r := &Recipe{}

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		if m := classline.FindStringSubmatch(line); m != nil {
			r.Repo = string(m[1])
		} else if m := versionline.FindStringSubmatch(line); m != nil {
			r.Version = string(m[1])
		} else if m := fileline.FindStringSubmatch(line); m != nil {
			//r.Files = append(r.Files, PackageFile(m[1]))
			// Ignore this for now
		} else if m := descline.FindStringSubmatch(line); m != nil {
			r.Description = string(m[1])
		}
	}

	return r, nil
}
