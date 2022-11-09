package program

import (
	"bufio"
	_ "embed"
	"github.com/deweysasser/releasetool/homebrew"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"text/template"
)

//go:embed section.md
var templateText string

var sectionTemplate = template.Must(template.New("section").Parse(templateText))

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
				// TODO:  put in an explicit end marker
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
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
