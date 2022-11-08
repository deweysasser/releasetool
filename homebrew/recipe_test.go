package homebrew

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"os"
	"strings"
	"testing"
)

//
//func Test_filterFiles(t *testing.T) {
//	brew := &Recipe{
//		Owner:       "deweysasser",
//		Repo:        "testing",
//		Version:     "v0.1.0",
//		Description: "test description",
//		Files: []PackageFile{
//			"../cumulus/dist/cumulus-darwin-amd64.zip",
//			"../cumulus/dist/cumulus-darwin-arm64.zip",
//			"../cumulus/dist/cumulus-linux-amd64.zip",
//			"../cumulus/dist/cumulus-linux-arm64.zip",
//			"../cumulus/dist/cumulus-windows-amd64.zip",
//			"../cumulus/dist/cumulus-windows-arm64.zip",
//		},
//	}
//
//	type args struct {
//		b     *Recipe
//		terms []string
//	}
//	tests := []struct {
//		name string
//		args args
//		want []PackageFile
//	}{
//		{
//			name: "basic",
//			args: args{b: brew,
//				terms: []string{"darwin", "amd64"},
//			},
//			want: []PackageFile{
//				"../cumulus/dist/cumulus-darwin-amd64.zip",
//			},
//		},
//		{
//			name: "several",
//			args: args{b: brew,
//				terms: []string{"darwin"},
//			},
//			want: []PackageFile{
//				"../cumulus/dist/cumulus-darwin-amd64.zip",
//				"../cumulus/dist/cumulus-darwin-arm64.zip",
//			},
//		},
//	}
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			assert.Equalf(t, tt.want, filterFiles(tt.args.b, tt.args.terms...), "filterFiles(%v, %v)", tt.args.b, tt.args.terms)
//		})
//	}
//}

func TestParseRecipeFile(t *testing.T) {
	current, err := ParseRecipeFile("parse_recipe_test.rb")
	assert.NoError(t, err)

	assert.Equal(t, "v0.2.0", current.Version)
	assert.Equal(t, "Cumulus", current.Repo)
	//assert.Equal(t, 4, len(current.Files))
}

func TestGenerateRecipe(t *testing.T) {
	exp, err := os.ReadFile("parse_recipe_test.rb")
	expected := string(exp)
	assert.NoError(t, err)

	current, err := ParseRecipeFile("parse_recipe_test.rb")
	assert.NoError(t, err)

	current.Repo = strings.ToLower(current.Repo)
	current.Owner = "deweysasser"

	buf := bytes.NewBuffer(nil)
	err = current.Generate(buf)
	assert.NoError(t, err)

	assert.Equal(t, expected, buf.String())
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
