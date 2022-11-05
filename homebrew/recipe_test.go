package homebrew

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_filterFiles(t *testing.T) {
	brew := &Recipe{
		Owner:       "deweysasser",
		Repo:        "testing",
		Version:     "v0.1.0",
		Description: "test description",
		Files: []PackageFile{
			"../cumulus/dist/cumulus-darwin-amd64.zip",
			"../cumulus/dist/cumulus-darwin-arm64.zip",
			"../cumulus/dist/cumulus-linux-amd64.zip",
			"../cumulus/dist/cumulus-linux-arm64.zip",
			"../cumulus/dist/cumulus-windows-amd64.zip",
			"../cumulus/dist/cumulus-windows-arm64.zip",
		},
	}

	type args struct {
		b     *Recipe
		terms []string
	}
	tests := []struct {
		name string
		args args
		want []PackageFile
	}{
		{
			name: "basic",
			args: args{b: brew,
				terms: []string{"darwin", "amd64"},
			},
			want: []PackageFile{
				"../cumulus/dist/cumulus-darwin-amd64.zip",
			},
		},
		{
			name: "several",
			args: args{b: brew,
				terms: []string{"darwin"},
			},
			want: []PackageFile{
				"../cumulus/dist/cumulus-darwin-amd64.zip",
				"../cumulus/dist/cumulus-darwin-arm64.zip",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, filterFiles(tt.args.b, tt.args.terms...), "filterFiles(%v, %v)", tt.args.b, tt.args.terms)
		})
	}
}
