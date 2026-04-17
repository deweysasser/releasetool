package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestNewConfigFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, `
owner: deweysasser
tap: deweysasser/tap
recipes:
  - repo: foo
    description: a tool
  - repo: deweysasser/bar
docs:
  - file: README.md
    section: "## Tools"
`)

	cf, err := NewConfigFile(&Brew{ConfigFile: path})
	require.NoError(t, err)
	assert.Equal(t, "deweysasser", cf.Owner)
	assert.Equal(t, "deweysasser/tap", cf.Tap)
	require.Len(t, cf.Recipes, 2)
	assert.Equal(t, "foo", cf.Recipes[0].Repo)
	assert.Equal(t, "a tool", cf.Recipes[0].Description)
	assert.Equal(t, "deweysasser/bar", cf.Recipes[1].Repo)
	require.Len(t, cf.Docs, 1)
	assert.Equal(t, "README.md", cf.Docs[0].File)
	assert.Equal(t, "## Tools", cf.Docs[0].Section)
}

func TestNewConfigFile_MissingFile(t *testing.T) {
	_, err := NewConfigFile(&Brew{ConfigFile: filepath.Join(t.TempDir(), "nope.yaml")})
	assert.Error(t, err)
}

func TestNewConfigFile_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	writeFile(t, path, "this: is: not: yaml\n::::\n")
	_, err := NewConfigFile(&Brew{ConfigFile: path})
	assert.Error(t, err)
}

func TestUpdate_AppendsSectionWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writeFile(t, path, "# Project\n\nIntro paragraph.\n")

	cf := &ConfigFile{Tap: "deweysasser/tap"}
	ud := UpdateDoc{File: path, Section: "## Tools"}

	require.NoError(t, ud.Update(cf, nil))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	// Intro is preserved.
	assert.Contains(t, string(out), "Intro paragraph.")
	// New section was appended.
	assert.Contains(t, string(out), "## Tools")
}

func TestUpdate_ReplacesSectionBetweenHeadings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	initial := `# Project

## Tools
STALE CONTENT THAT SHOULD BE REPLACED
MORE STALE CONTENT

## Other
preserved trailing section
`
	writeFile(t, path, initial)

	cf := &ConfigFile{Tap: "deweysasser/tap"}
	ud := UpdateDoc{File: path, Section: "## Tools"}

	require.NoError(t, ud.Update(cf, nil))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	s := string(out)

	assert.NotContains(t, s, "STALE CONTENT")
	assert.NotContains(t, s, "MORE STALE CONTENT")
	assert.Contains(t, s, "## Tools")
	assert.Contains(t, s, "## Other")
	assert.Contains(t, s, "preserved trailing section")
}

func TestUpdate_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writeFile(t, path, "# Project\n")

	cf := &ConfigFile{Tap: "t"}
	ud := UpdateDoc{File: path, Section: "## Tools"}

	require.NoError(t, ud.Update(cf, nil))

	_, err := os.Stat(path + ".bak")
	assert.NoError(t, err, "expected backup file to be created")
}

func TestUpdate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writeFile(t, path, `# Project

## Tools

## End
`)

	cf := &ConfigFile{Tap: "t"}
	ud := UpdateDoc{File: path, Section: "## Tools"}

	require.NoError(t, ud.Update(cf, nil))
	first, err := os.ReadFile(path)
	require.NoError(t, err)

	require.NoError(t, ud.Update(cf, nil))
	second, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second),
		"Update should be idempotent when run twice with the same input")
}

func TestUpdate_FileMissing(t *testing.T) {
	ud := UpdateDoc{File: filepath.Join(t.TempDir(), "nope.md"), Section: "## Tools"}
	err := ud.Update(&ConfigFile{}, nil)
	assert.Error(t, err)
}

// TestUpdate_LegacySectionWithNoTrailingHeading documents the first-run
// behavior on a legacy file that has no end marker and no trailing heading:
// the section content is consumed, and the marker is written on output so
// subsequent runs have a deterministic boundary.
func TestUpdate_LegacySectionWithNoTrailingHeading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writeFile(t, path, `# Project

## Tools
this content should terminate somewhere
but there is no following heading
`)

	cf := &ConfigFile{Tap: "t"}
	ud := UpdateDoc{File: path, Section: "## Tools"}
	require.NoError(t, ud.Update(cf, nil))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "# Project")
	assert.Contains(t, s, "## Tools")
	assert.NotContains(t, s, "this content should terminate somewhere",
		"first-pass legacy behavior: content in a section with no terminator is consumed")
	assert.Contains(t, s, sectionEndMarker,
		"output must include the end marker so future runs terminate precisely")
}

// TestUpdate_EndMarkerStopsBeforeLaterHeadings verifies that when the file
// already contains the end marker, the managed region ends at the marker —
// not at the next heading. Content after the marker is preserved verbatim.
func TestUpdate_EndMarkerStopsBeforeLaterHeadings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writeFile(t, path, `# Project

## Tools
OLD MANAGED CONTENT
`+sectionEndMarker+`

This paragraph sits outside the managed section and must be preserved.

## Other
also preserved
`)

	cf := &ConfigFile{Tap: "t"}
	ud := UpdateDoc{File: path, Section: "## Tools"}
	require.NoError(t, ud.Update(cf, nil))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	s := string(out)

	assert.NotContains(t, s, "OLD MANAGED CONTENT",
		"old managed content inside the marker region must be replaced")
	assert.Contains(t, s, "This paragraph sits outside the managed section and must be preserved.",
		"content after the end marker must survive the update")
	assert.Contains(t, s, "## Other")
	assert.Contains(t, s, "also preserved")
	assert.Equal(t, 1, strings.Count(s, sectionEndMarker),
		"exactly one end marker must remain after update")
}

// TestUpdate_PreservesContentAcrossTwoRuns checks that once a file has been
// updated once (gaining a marker), a second run is a pure no-op even if the
// user adds text after the marker.
func TestUpdate_PreservesContentAcrossTwoRuns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writeFile(t, path, "# Project\n\n## Tools\n")

	cf := &ConfigFile{Tap: "t"}
	ud := UpdateDoc{File: path, Section: "## Tools"}
	require.NoError(t, ud.Update(cf, nil))

	// User appends notes after the managed section.
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	augmented := string(current) + "\nSome user-written notes that must persist.\n"
	require.NoError(t, os.WriteFile(path, []byte(augmented), 0o644))

	require.NoError(t, ud.Update(cf, nil))

	final, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(final), "Some user-written notes that must persist.",
		"post-marker content added by the user must survive subsequent updates")
}
