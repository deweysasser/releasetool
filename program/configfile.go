package program

import (
	"bufio"
	_ "embed"
	"fmt"
	"github.com/deweysasser/releasetool/homebrew"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed section.md
var templateText string

var sectionTemplate = template.Must(template.New("section").Parse(templateText))

// sectionEndMarker terminates a releasetool-managed section. The section
// template always emits this marker so subsequent Update runs have a
// deterministic boundary. If the marker is absent (legacy files), Update
// falls back to terminating at the next markdown heading.
const sectionEndMarker = "<!-- releasetool:end -->"

type UpdateDoc struct {
	// File is the name of the file to update
	File string `json:"file"`
	// Section is the name of the section to replace
	Section string `json:"section"`
}

type ConfigFile struct {
	// Owner is the default owner for all repos, if an owner is not specified
	Owner   string             `json:"owner"`
	Recipes []*homebrew.Recipe `json:"recipes"`
	// Docs is the list of documents to update with the recipes
	Docs []UpdateDoc `json:"docs"`
	// Tap should be the prefix for this tap
	Tap string `json:"tap"`
}

// validateDocFile rejects UpdateDoc.File paths that would escape the
// working directory or target an absolute path. The file comes from a
// YAML config, so a hostile config could otherwise steer writes to any
// file the user can modify.
func validateDocFile(p string) error {
	if p == "" {
		return errors.New("UpdateDoc.File must not be empty")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("UpdateDoc.File %q must be a relative path within the working directory", p)
	}
	cleaned := filepath.Clean(p)
	// filepath.Clean collapses ./a/../b → b, so if ".." is still present
	// after Clean, the path escapes the working directory.
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, `..\`) {
		return fmt.Errorf("UpdateDoc.File %q escapes the working directory", p)
	}
	return nil
}

func NewConfigFile(b *Brew) (*ConfigFile, error) {
	list := &ConfigFile{}
	bytes, err := os.ReadFile(b.ConfigFile)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(bytes, list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (u *UpdateDoc) Update(configfile *ConfigFile, recipes []*homebrew.Recipe) error {

	// Refuse absolute paths and anything that walks out of the working
	// directory. A YAML config drives this path — without the check, a
	// malicious config can patch arbitrary files the user has write
	// access to (e.g. ~/.ssh/authorized_keys, /etc/crontab on shared
	// hosts).
	if err := validateDocFile(u.File); err != nil {
		return err
	}

	f, err := os.Open(u.File)
	tmp := u.File + ".tmp"
	if err != nil {
		return err
	}

	defer f.Close()

	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)

	scanner := bufio.NewScanner(f)

	scanner.Split(bufio.ScanLines)

	found := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == u.Section {
			found = true
			if _, err := out.Write([]byte(line)); err != nil {
				return err
			}
			if _, err := out.Write([]byte("\n")); err != nil {
				return err
			}

			err = emitTemplate(out, configfile, recipes)
			if err != nil {
				return err
			}

			for scanner.Scan() {
				line := scanner.Text()
				trimmed := strings.TrimSpace(line)
				// Marker terminates the section and is consumed; the template
				// re-emits a fresh one.
				if trimmed == sectionEndMarker {
					break
				}
				// Fallback for legacy files without a marker: the next
				// markdown heading terminates the section and is preserved.
				if strings.HasPrefix(trimmed, "#") {
					if _, err := out.Write([]byte(line)); err != nil {
						return err
					}
					if _, err := out.Write([]byte("\n")); err != nil {
						return err
					}
					break
				}
			}
			break
		} else {
			if _, err := out.Write([]byte(line)); err != nil {
				return err
			}
			if _, err := out.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}

	if !found {
		if _, err := out.Write([]byte(u.Section)); err != nil {
			return err
		}

		err = emitTemplate(out, configfile, recipes)
		if err != nil {
			return err
		}
	}

	// Append the remaining file
	for scanner.Scan() {
		if _, err := out.Write(scanner.Bytes()); err != nil {
			return err
		}
		if _, err := out.Write([]byte("\n")); err != nil {
			return err
		}
	}

	f.Close() // close the file
	out.Close()
	backup := u.File + ".bak"
	if _, err := os.Stat(backup); err == nil {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}

	if err := os.Rename(u.File, u.File+".bak"); err != nil {
		return err
	}

	// From here out, error recovery would involve renaming the backup file back to the original

	if err := os.Rename(tmp, u.File); err != nil {
		if e2 := os.Rename(backup, u.File); e2 != nil {
			return errors.Wrap(
				err,
				"unable to rename backup back to main file name",
			)
		}
		return err
	}

	return nil
}

func emitTemplate(out *os.File, file *ConfigFile, recipes []*homebrew.Recipe) error {
	data := map[string]any{
		"config":  file,
		"recipes": recipes,
	}

	return sectionTemplate.ExecuteTemplate(out, "section", data)
}
