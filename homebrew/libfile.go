package homebrew

import (
	_ "embed"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
)

//go:embed private_strategy.rb
var private_access_lib string

func WriteLibFile() error {
	path := "lib/private_access.rb"

	_, err := os.Stat(path)
	if err == nil {
		log.Debug().Str("path", path).Msg("Lib file already exists")
		return nil
	}

	log.Debug().Str("path", path).Msg("Writing lib file")

	dir := filepath.Dir(path)

	_, err = os.Stat(dir)

	if err != nil {
		// 0755 (rwxr-xr-x) lets the owner traverse and modify while
		// preventing world-writable access on shared hosts. os.ModePerm
		// (0777) would allow any local user to replace the Ruby that
		// gets `require_relative`'d at `brew install` time.
		err = os.Mkdir(dir, 0o755)
		if err != nil {
			return err
		}
	}

	// 0644 (rw-r--r--) — owner writes, world reads. Homebrew loads this
	// file as the invoking user, so world-readable is fine; world-writable
	// is a code-execution risk (see mkdir comment above).
	return os.WriteFile(path, []byte(private_access_lib), 0o644)
}
