package homebrew

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseRecipeFile(t *testing.T) {
	current, err := ParseRecipeFile("parse_recipe_test.rb")
	require.NoError(t, err)

	assert.Equal(t, "v0.5.0", current.Version)
	assert.Equal(t, "Cumulus", current.Repo)
	assert.Equal(t, "A better AWS (and other cloud) CLI", current.Description)
}

func TestNewRecipe(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		owner     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "explicit owner and repo",
			repo:      "cumulus",
			owner:     "deweysasser",
			wantOwner: "deweysasser",
			wantRepo:  "cumulus",
		},
		{
			name:      "owner inferred from owner/repo form",
			repo:      "deweysasser/cumulus",
			owner:     "",
			wantOwner: "deweysasser",
			wantRepo:  "cumulus",
		},
		{
			name:    "missing owner and bare repo returns error",
			repo:    "cumulus",
			owner:   "",
			wantErr: true,
		},
		{
			name:    "missing owner and multi-slash repo returns error",
			repo:    "a/b/c",
			owner:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRecipe(tt.repo, tt.owner, "v1.0.0", "desc")
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, r)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, tt.wantOwner, r.Owner)
			assert.Equal(t, tt.wantRepo, r.Repo)
			assert.Equal(t, "v1.0.0", r.Version)
			assert.Equal(t, "desc", r.Description)
			assert.NotNil(t, r.Files)
		})
	}
}

func TestVersionFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"v1.2.0", "1.2.0"},
		{"V1.2.0", "1.2.0"},
		{"1.2.0", "1.2.0"},
		{"v1.2.0-rc1", "1.2.0-rc1"},
		{"v1.2.0-rc.1", "1.2.0-rc.1"}, // semver-style dotted identifier is preserved verbatim
		{"v1.2.0-alpha.1", "1.2.0-alpha.1"},
		{"V2", "2"},
		{"", ""},
		{"version-1", "ersion-1"}, // only a single leading v/V is stripped; documents the contract
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, versionFilename(tt.in))
		})
	}
}

func TestVersionedClass(t *testing.T) {
	// The expected values here are what Homebrew's Formulary.class_s
	// computes from the filename stem (e.g. "cumulus@1.2.0-rc.1"). Homebrew
	// refuses to load a formula whose class name does not match that value,
	// so any drift from class_s is a load-time error for the end user.
	tests := []struct {
		repo, version, want string
	}{
		{"cumulus", "v1.2.0", "CumulusAT120"},
		{"cumulus", "1.2.0", "CumulusAT120"},
		{"my-tool", "v1.2.0-rc1", "MyToolAT120Rc1"},
		{"some.tool", "v1.0.0", "SomeToolAT100"},
		// Semver-style dotted prerelease identifiers: every separator
		// (dash or dot) causes the following alphanumeric to be uppercased,
		// matching Homebrew's class_s transform.
		{"cumulus", "v1.2.0-rc.1", "CumulusAT120Rc1"},
		{"cumulus", "v1.2.0-alpha.1", "CumulusAT120Alpha1"},
		{"cumulus", "v1.2.0-alpha.beta", "CumulusAT120AlphaBeta"},
		// Regression: the exact case that failed on a user's Mac when
		// `brew install releasetool@0.5.0-rc.1` refused to load the
		// formula because its class name disagreed with class_s.
		{"releasetool", "v0.5.0-rc.1", "ReleasetoolAT050Rc1"},
	}
	for _, tt := range tests {
		t.Run(tt.repo+"_"+tt.version, func(t *testing.T) {
			assert.Equal(t, tt.want, versionedClass(tt.repo, tt.version))
		})
	}
}

func TestIsPrereleaseTag(t *testing.T) {
	prereleases := []string{
		// Bare suffixes and numeric-only continuations.
		"v1.0.0-rc1",
		"v1.0.0-rc",
		"v1.0.0-alpha",
		"v1.0.0-pre",
		"1.0.0-rc1",
		// Semver-style dotted identifiers.
		"v1.0.0-rc.1",
		"v1.0.0-RC.2",
		"v1.0.0-alpha.1",
		"v1.0.0-beta.2",
		"v1.0.0-pre.1",
		"v1.0.0-alpha.beta",
		"v1.0.0-rc.0.1",
		"v1.0.0-alpha.1.2.3",
	}
	stable := []string{
		"v1.0.0",
		"v1.0.0.1",
		"1.2.3",
		"v2",
		// "-final" / "-release" are not prerelease markers by convention.
		"v1.0.0-final",
		"v1.0.0-release.1",
	}
	for _, tag := range prereleases {
		t.Run("pre/"+tag, func(t *testing.T) {
			assert.True(t, isPrereleaseTag(tag), "%q should be a prerelease tag", tag)
		})
	}
	for _, tag := range stable {
		t.Run("stable/"+tag, func(t *testing.T) {
			assert.False(t, isPrereleaseTag(tag), "%q should not be a prerelease tag", tag)
		})
	}
}

func TestExpandVersions_MixedReleases(t *testing.T) {
	base := &Recipe{Owner: "o", Repo: "cumulus", Description: "d"}
	releases := []ReleaseInfo{
		{Version: "v1.2.0-rc1", Prerelease: true},
		{Version: "v1.1.0", Prerelease: false},
		{Version: "v1.0.0", Prerelease: false},
	}

	out := ExpandVersions(base, releases)

	require.Len(t, out, 4, "3 versioned + 1 default")

	// Versioned recipes come first, in release order.
	assert.Equal(t, "cumulus@1.2.0-rc1.rb", out[0].OutputFile)
	assert.Equal(t, "CumulusAT120Rc1", out[0].ClassName)
	assert.Equal(t, "v1.2.0-rc1", out[0].Version)
	assert.True(t, out[0].Prerelease)

	assert.Equal(t, "cumulus@1.1.0.rb", out[1].OutputFile)
	assert.Equal(t, "CumulusAT110", out[1].ClassName)
	assert.False(t, out[1].Prerelease)

	assert.Equal(t, "cumulus@1.0.0.rb", out[2].OutputFile)
	assert.Equal(t, "CumulusAT100", out[2].ClassName)

	// Default is the newest non-prerelease (v1.1.0, not the rc).
	assert.Equal(t, "cumulus.rb", out[3].OutputFile)
	assert.Equal(t, "Cumulus", out[3].ClassName)
	assert.Equal(t, "v1.1.0", out[3].Version)
	assert.False(t, out[3].Prerelease)
}

func TestExpandVersions_OnlyPrereleases(t *testing.T) {
	base := &Recipe{Owner: "o", Repo: "tool"}
	releases := []ReleaseInfo{
		{Version: "v1.0.0-rc2", Prerelease: true},
		{Version: "v1.0.0-rc1", Prerelease: true},
	}

	out := ExpandVersions(base, releases)

	require.Len(t, out, 2, "no default when there are no stable releases")
	for _, r := range out {
		assert.Contains(t, r.OutputFile, "@",
			"all outputs must be versioned when no stable exists; no bare tool.rb")
	}
}

func TestExpandVersions_NoReleases(t *testing.T) {
	base := &Recipe{Owner: "o", Repo: "tool"}
	out := ExpandVersions(base, nil)
	assert.Empty(t, out)
}

func TestExpandVersions_CopiesBaseFields(t *testing.T) {
	base := &Recipe{Owner: "o", Repo: "tool", Description: "desc", PrivateRepo: true}
	releases := []ReleaseInfo{
		{Version: "v1.0.0", Prerelease: false},
	}

	out := ExpandVersions(base, releases)
	require.Len(t, out, 2)
	for _, r := range out {
		assert.Equal(t, "o", r.Owner)
		assert.Equal(t, "tool", r.Repo)
		assert.Equal(t, "desc", r.Description)
		assert.True(t, r.PrivateRepo)
	}
}

func TestRecipe_Normalize(t *testing.T) {
	type fields struct {
	}
	tests := []struct {
		name   string
		Recipe Recipe
		Owner  string
		Repo   string
	}{
		{"nothing needed", Recipe{Owner: "o", Repo: "r"}, "o", "r"},
		{"normalization", Recipe{Repo: "o/r"}, "o", "r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.Recipe
			r.Normalize()
			assert.Equal(t, tt.Owner, r.Owner)
			assert.Equal(t, tt.Repo, r.Repo)
		})
	}
}
