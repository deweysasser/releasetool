package homebrew

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"github.com/deweysasser/releasetool/timing"
	"github.com/google/go-github/v84/github"
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
	// ClassName is the Ruby class name used in the rendered formula. It
	// must match what Homebrew's Formulary.class_s derives from the
	// formula's filename (e.g. "cumulus@1.2.0-rc.1.rb" -> CumulusAT120Rc1),
	// otherwise Homebrew refuses to load the formula.
	ClassName string `json:"-" yaml:"-"`
	// OutputFile is the target filename for the generated .rb — "cumulus.rb"
	// for the default and "cumulus@1.2.0.rb" for versioned formulas.
	OutputFile string `json:"-" yaml:"-"`
	// Prerelease is true when this recipe represents a prerelease version
	// (either GitHub's flag or an -rc/-alpha/-beta/-pre tag).
	Prerelease bool `json:"-" yaml:"-"`
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

// Validate returns an error when fields that will be interpolated into
// filesystem paths contain characters that could escape the working
// directory. Owner and Repo land in the output filename
// ({Repo}.rb / {Repo}@{Version}.rb) and must be safe basename components.
// Called after Normalize — so by the time we check, a valid "owner/repo"
// has already been split and any path separator in Repo is a red flag.
func (r *Recipe) Validate() error {
	if err := validatePathComponent("owner", r.Owner); err != nil {
		return err
	}
	if err := validatePathComponent("repo", r.Repo); err != nil {
		return err
	}
	return nil
}

// validatePathComponent rejects empty values and anything that would
// change the meaning of a path: `..`, leading/embedded separators, NUL.
// GitHub owners and repo names are constrained to a small alphanumeric
// set with `-` and `_`, so legitimate values pass trivially.
func validatePathComponent(field, v string) error {
	if v == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if v == "." || v == ".." {
		return fmt.Errorf("%s must not be %q", field, v)
	}
	if strings.ContainsAny(v, "/\\\x00") {
		return fmt.Errorf("%s %q contains a path separator or NUL", field, v)
	}
	return nil
}

// newGithubClient builds the github client used by FillFromGithub. It is a
// package-level variable so tests can swap it for one pointed at httptest.Server.
var newGithubClient = func() *github.Client {
	return github.NewClient(githubHttpClient())
}

// ReleaseInfo is the per-release data returned by FetchReleases: the tag
// name, whether GitHub (or the tag convention) marks it as a prerelease, and
// the downloadable assets that a Homebrew formula needs.
type ReleaseInfo struct {
	Version    string
	Prerelease bool
	Files      []PackageFile
}

// FetchReleases returns every release on the repo by paging through the
// GitHub ListReleases endpoint until NextPage == 0. The order matches
// GitHub's response (newest first). Repo description and private-repo
// detection also happen here so callers only need a single trip.
func (b *Recipe) FetchReleases() ([]ReleaseInfo, error) {
	defer timing.Start("FetchReleases " + b.Owner + "/" + b.Repo).Done()
	client := newGithubClient()
	ctx := context.Background()

	repo, _, err := client.Repositories.Get(ctx, b.Owner, b.Repo)
	if err != nil {
		return nil, err
	}

	if b.Description == "" {
		b.Description = repo.GetDescription()
	}
	b.PrivateRepo = repo.GetPrivate()

	var all []ReleaseInfo
	opts := &github.ListOptions{PerPage: 100}
	for {
		page, resp, err := client.Repositories.ListReleases(ctx, b.Owner, b.Repo, opts)
		if err != nil {
			return nil, err
		}
		for _, release := range page {
			tag := release.GetTagName()
			log.Debug().Str("tag", tag).Bool("prerelease", release.GetPrerelease()).Msg("Found release")
			info := ReleaseInfo{
				Version:    tag,
				Prerelease: release.GetPrerelease() || isPrereleaseTag(tag),
			}
			for _, asset := range release.Assets {
				log.Debug().
					Str("name", asset.GetName()).
					Str("browser_url", asset.GetBrowserDownloadURL()).
					Str("asset_url", asset.GetURL()).
					Int("size", asset.GetSize()).
					Bool("isPrivate", b.PrivateRepo).
					Msg("Found asset")
				info.Files = append(info.Files, PackageFile{b.PrivateRepo, asset})
			}
			all = append(all, info)
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return all, nil
}

// ExpandVersions produces one *Recipe per release plus — when at least one
// release is non-prerelease — a default unversioned recipe pointing at the
// newest non-prerelease. Versioned recipes come first in release order
// (newest first, matching GitHub); the default (if any) is appended last.
//
// Each returned recipe has OutputFile, ClassName, Version, Prerelease, and
// Files set; Owner, Repo, Description, and PrivateRepo are copied from base.
func ExpandVersions(base *Recipe, releases []ReleaseInfo) []*Recipe {
	if len(releases) == 0 {
		return nil
	}

	out := make([]*Recipe, 0, len(releases)+1)

	var defaultRelease *ReleaseInfo
	for i, rel := range releases {
		sub := cloneBase(base)
		sub.Version = rel.Version
		sub.Prerelease = rel.Prerelease
		sub.Files = append([]PackageFile(nil), rel.Files...)
		sub.OutputFile = base.Repo + "@" + versionFilename(rel.Version) + ".rb"
		sub.ClassName = versionedClass(base.Repo, rel.Version)
		out = append(out, sub)

		if defaultRelease == nil && !rel.Prerelease {
			defaultRelease = &releases[i]
		}
	}

	if defaultRelease == nil {
		log.Warn().
			Str("repo", base.Repo).
			Msg("No non-prerelease releases found; skipping default unversioned formula")
		return out
	}

	def := cloneBase(base)
	def.Version = defaultRelease.Version
	def.Prerelease = false
	def.Files = append([]PackageFile(nil), defaultRelease.Files...)
	def.OutputFile = base.Repo + ".rb"
	def.ClassName = homebrewClassName(base.Repo)
	out = append(out, def)

	return out
}

func cloneBase(base *Recipe) *Recipe {
	return &Recipe{
		Owner:       base.Owner,
		Repo:        base.Repo,
		Description: base.Description,
		PrivateRepo: base.PrivateRepo,
	}
}

// FillFromGithub populates the recipe with the default version — the newest
// non-prerelease. If the repo has only prereleases, it falls back to the
// newest release overall so callers that rely on the single-recipe code path
// still get something to generate.
func (b *Recipe) FillFromGithub() error {
	releases, err := b.FetchReleases()
	if err != nil {
		return err
	}
	if len(releases) < 1 {
		return errors.New("no release found")
	}

	chosen := releases[0]
	for _, r := range releases {
		if !r.Prerelease {
			chosen = r
			break
		}
	}

	b.Version = chosen.Version
	b.Files = append(b.Files, chosen.Files...)
	return nil
}

// configuredToken, when non-empty, is the bearer token used for GitHub
// API calls. Set by the CLI via SetToken so callers can source it from
// places the homebrew package itself does not know about (e.g. `gh auth
// token`). When empty, githubHttpClient falls back to env lookups.
var configuredToken string

// tokenAuthDisabled suppresses all authentication, including env-var
// fallback. Set by SetAuthDisabled for the CLI's --dont-use-token mode.
var tokenAuthDisabled bool

// SetToken records the bearer token used for authenticated GitHub API
// calls. Pass "" to defer to environment-variable detection.
func SetToken(t string) { configuredToken = t }

// SetAuthDisabled forces all subsequent GitHub API calls to be made
// unauthenticated, ignoring both the configured token and any env vars.
func SetAuthDisabled(b bool) { tokenAuthDisabled = b }

// AuthStateForTest returns the current auth configuration so higher-level
// tests can verify that credential resolution landed the expected values
// in the homebrew package without issuing a network request.
func AuthStateForTest() (token string, disabled bool) {
	return configuredToken, tokenAuthDisabled
}

func githubHttpClient() *http.Client {
	client := buildBaseGithubHttpClient()
	// Wrap the transport so both go-github's API calls and our raw
	// asset downloads in files.go benefit from a single rate-limit
	// retry policy. Wrapping the oauth2 transport (not wrapping oauth2
	// around our retry) means the bearer token is re-applied on each
	// retry; oauth2.Transport already clones the request before adding
	// the header, so successive retries don't stack the header.
	client.Transport = newRateLimitRetryTransport(client.Transport)
	return client
}

func buildBaseGithubHttpClient() *http.Client {
	if tokenAuthDisabled {
		return &http.Client{Transport: http.DefaultTransport}
	}

	token := configuredToken
	source := "SetToken"
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
		source = "GITHUB_TOKEN"
	}
	if token == "" {
		token = os.Getenv("GH_TOKEN")
		source = "GH_TOKEN"
	}
	if token == "" {
		return &http.Client{Transport: http.DefaultTransport}
	}

	log.Debug().Str("source", source).Msg("Using bearer token for GitHub API")
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	return oauth2.NewClient(ctx, ts)
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
			"files":      filterFiles,
			"token":      tokenWordsOnly,
			"title":      titleCase,
			"camelcase":  camelCase,
			"upper":      strings.ToUpper,
			"lower":      strings.ToLower,
			"basename":   filepath.Base,
			"rubystring": rubyStringEscape,
		}).
		Parse(recipe)
	if err != nil {
		panic(err)
	}

	recipeTemplate = temp
}

func (r *Recipe) Generate(output io.Writer) error {
	log.Debug().Str("name", r.Repo).Str("version", r.Version).Msg("Generating recipe")
	return recipeTemplate.Execute(output, r)
}

// These two must be complimentary sets
var allowedLetters = regexp.MustCompile("[a-zA-Z0-9_]+")
var disallowedLetters = regexp.MustCompile("[^a-zA-Z0-9_]")

// prereleaseSuffix matches tag suffixes that indicate a prerelease even when
// GitHub's own prerelease flag is not set (some repos tag -rc / -beta without
// checking the box). It anchors to the end of the tag so trailing numeric
// components like "-rc.2" or "-beta1" still match.
var prereleaseSuffix = regexp.MustCompile(`(?i)-(rc|alpha|beta|pre)([.\-][a-z0-9]+|[0-9]+)*$`)

// versionFilename strips a single leading "v" or "V" from a tag so it can be
// used inside a Homebrew versioned-formula filename like "tool@1.2.0.rb".
func versionFilename(version string) string {
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return version[1:]
	}
	return version
}

// versionedClass returns the Homebrew versioned-formula class name — e.g.
// (repo="cumulus", version="v1.2.0-rc.1") -> "CumulusAT120Rc1". It builds
// the filename stem ("repo@version") and runs it through the class_s
// transform so the result matches exactly what Homebrew expects when it
// loads "repo@version.rb".
func versionedClass(repo, version string) string {
	return homebrewClassName(repo + "@" + versionFilename(version))
}

// homebrewSeparatorRE matches Homebrew's class_s separator set
// (/[-_.\s]([a-zA-Z0-9])/): any hyphen, underscore, dot, or whitespace
// followed by a single alphanumeric character.
var homebrewSeparatorRE = regexp.MustCompile(`[-_.\s]([a-zA-Z0-9])`)

// homebrewATRE matches Homebrew's class_s versioned-formula marker
// (/(.)@(\d)/): any single character, then "@", then a single digit.
var homebrewATRE = regexp.MustCompile(`(.)@(\d)`)

// homebrewClassName replicates Ruby Homebrew's Formulary.class_s. Matching
// it exactly is load-bearing: Homebrew derives the expected class name
// from the .rb filename via this same transform and refuses to load the
// formula when the class name in the file disagrees.
//
// Reference (Library/Homebrew/formulary.rb):
//
//	class_name = name.capitalize
//	class_name.gsub!(/[-_.\s]([a-zA-Z0-9])/) { Regexp.last_match(1).upcase }
//	class_name.tr!("+", "x")
//	class_name.sub!(/(.)@(\d)/, '\1AT\2')
func homebrewClassName(name string) string {
	if name == "" {
		return ""
	}
	// Ruby's String#capitalize: first char upper, the rest lower.
	s := strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
	// Replace each "<sep><alnum>" with the uppercased <alnum>.
	s = homebrewSeparatorRE.ReplaceAllStringFunc(s, func(m string) string {
		return strings.ToUpper(m[len(m)-1:])
	})
	s = strings.ReplaceAll(s, "+", "x")
	// Ruby's sub! replaces only the first match; do the same here so a
	// second "@<digit>" (pathological but legal in a filename) is left
	// alone.
	if loc := homebrewATRE.FindStringSubmatchIndex(s); loc != nil {
		s = s[:loc[0]] + s[loc[2]:loc[3]] + "AT" + s[loc[4]:loc[5]] + s[loc[1]:]
	}
	return s
}

// isPrereleaseTag returns true when a git tag looks like a prerelease by
// convention (-rc, -alpha, -beta, -pre, optionally followed by a number or
// dotted/dashed identifier).
func isPrereleaseTag(tag string) bool {
	return prereleaseSuffix.MatchString(tag)
}

func tokenWordsOnly(s string) string {
	return disallowedLetters.ReplaceAllString(s, "")
}

// rubyStringEscape escapes a string for safe interpolation inside a Ruby
// double-quoted literal. Without this, a GitHub repo description (or any
// other user-controlled field the template writes into "..." in Ruby) can
// break out of the string and inject arbitrary Ruby that executes when
// `brew install` loads the generated formula — a supply-chain hole from
// the repo owner to every tap user.
//
// Ruby's double-quoted strings are vulnerable to backslash escapes and to
// unescaped double quotes; they also process `#{...}` interpolation. We
// neutralize all three:
//
//   - `\` → `\\`  (run first, or the replacements below would be re-escaped)
//   - `"` → `\"`
//   - `#` → `\#`  (defangs `#{...}` without disturbing isolated `#` chars)
//
// Newlines and other control characters remain as-is; Ruby accepts them
// inside double-quoted literals, and stripping them would corrupt
// legitimate multi-line descriptions.
func rubyStringEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `#`, `\#`)
	return s
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
