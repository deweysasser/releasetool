package homebrew

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseRecipeFile(t *testing.T) {
	current, err := ParseRecipeFile("parse_recipe_test.rb")
	require.NoError(t, err)

	assert.Equal(t, "v0.5.0", current.Version)
	assert.Equal(t, "Cumulus", current.Repo)
	assert.Equal(t, "A better AWS (and other cloud) CLI", current.Description)
}

func TestNewRecipe(t *testing.T) {
	tests := []struct {
		name        string
		repo        string
		owner       string
		wantOwner   string
		wantRepo    string
		wantErr     bool
	}{
		{
			name:      "explicit owner and repo",
			repo:      "cumulus",
			owner:     "deweysasser",
			wantOwner: "deweysasser",
			wantRepo:  "cumulus",
		},
		{
			name:      "owner inferred from owner/repo form",
			repo:      "deweysasser/cumulus",
			owner:     "",
			wantOwner: "deweysasser",
			wantRepo:  "cumulus",
		},
		{
			name:    "missing owner and bare repo returns error",
			repo:    "cumulus",
			owner:   "",
			wantErr: true,
		},
		{
			name:    "missing owner and multi-slash repo returns error",
			repo:    "a/b/c",
			owner:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRecipe(tt.repo, tt.owner, "v1.0.0", "desc")
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, r)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, tt.wantOwner, r.Owner)
			assert.Equal(t, tt.wantRepo, r.Repo)
			assert.Equal(t, "v1.0.0", r.Version)
			assert.Equal(t, "desc", r.Description)
			assert.NotNil(t, r.Files)
		})
	}
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
