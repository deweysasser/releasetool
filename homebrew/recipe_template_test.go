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
