package homebrew

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteLibFile_CreatesFileAndDir(t *testing.T) {
	t.Chdir(t.TempDir())

	require.NoError(t, WriteLibFile())

	info, err := os.Stat("lib")
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "expected lib/ to be a directory")

	contents, err := os.ReadFile(filepath.Join("lib", "private_access.rb"))
	require.NoError(t, err)
	assert.NotEmpty(t, contents, "expected written file to contain the embedded strategy")
	assert.Equal(t, private_access_lib, string(contents))
}

func TestWriteLibFile_SkipsWhenFileExists(t *testing.T) {
	t.Chdir(t.TempDir())

	require.NoError(t, os.Mkdir("lib", 0o755))
	sentinel := []byte("// user content — must not be overwritten\n")
	require.NoError(t, os.WriteFile(filepath.Join("lib", "private_access.rb"), sentinel, 0o644))

	require.NoError(t, WriteLibFile())

	got, err := os.ReadFile(filepath.Join("lib", "private_access.rb"))
	require.NoError(t, err)
	assert.Equal(t, sentinel, got, "existing file must not be overwritten")
}
