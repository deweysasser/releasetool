package homebrew

import "github.com/google/go-github/v84/github"

// SetNewGithubClientForTest swaps the package-level GitHub client constructor
// used by FetchReleases / FillFromGithub and returns a restore function that
// the caller MUST defer. Tests in other packages use this to point the code
// at an httptest.Server. Production code should never call this.
func SetNewGithubClientForTest(make func() *github.Client) func() {
	orig := newGithubClient
	newGithubClient = make
	return func() { newGithubClient = orig }
}
