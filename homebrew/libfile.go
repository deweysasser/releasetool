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
		err = os.Mkdir(dir, os.ModePerm)
		if err != nil {
			return err
		}
	}

	return os.WriteFile(path, []byte(private_access_lib), os.ModePerm)
}
