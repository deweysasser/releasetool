package program

import (
	"errors"
	"testing"

	"github.com/deweysasser/releasetool/homebrew"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetAuth clears the package-level auth state in the homebrew package
// both NOW and on cleanup, so each test starts from a clean slate no
// matter what ran before it (including TestBrew_Run_* tests that call
// resolveGithubCredentials as part of the normal Brew.Run flow).
func resetAuth(t *testing.T) {
	t.Helper()
	homebrew.SetToken("")
	homebrew.SetAuthDisabled(false)
	t.Cleanup(func() {
		homebrew.SetToken("")
		homebrew.SetAuthDisabled(false)
	})
}

// withStubGhAuthToken replaces the `gh auth token` exec with a stub so
// unit tests don't depend on the gh CLI actually being installed.
func withStubGhAuthToken(t *testing.T, token string, err error) {
	t.Helper()
	prev := ghAuthToken
	ghAuthToken = func() (string, error) { return token, err }
	t.Cleanup(func() { ghAuthToken = prev })
}

func TestResolveGithubCredentials_DontUseTokenWinsOverEnv(t *testing.T) {
	resetAuth(t)
	t.Setenv("GITHUB_TOKEN", "should-be-ignored")
	t.Setenv("GH_TOKEN", "also-ignored")
	withStubGhAuthToken(t, "from-gh-cli", nil)

	resolveGithubCredentials(true)

	tok, disabled := homebrew.AuthStateForTest()
	assert.True(t, disabled, "--dont-use-token must set auth-disabled")
	assert.Empty(t, tok, "--dont-use-token must not leave a configured token behind")
}

func TestResolveGithubCredentials_PrefersGITHUB_TOKEN(t *testing.T) {
	resetAuth(t)
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	t.Setenv("GH_TOKEN", "should-not-win")
	withStubGhAuthToken(t, "from-gh-cli", nil)

	resolveGithubCredentials(false)

	tok, disabled := homebrew.AuthStateForTest()
	assert.False(t, disabled)
	assert.Equal(t, "from-github-token", tok,
		"GITHUB_TOKEN must win over GH_TOKEN and gh auth token")
}

func TestResolveGithubCredentials_FallsBackToGH_TOKEN(t *testing.T) {
	resetAuth(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "from-gh-token-env")
	withStubGhAuthToken(t, "from-gh-cli", nil)

	resolveGithubCredentials(false)

	tok, _ := homebrew.AuthStateForTest()
	assert.Equal(t, "from-gh-token-env", tok,
		"GH_TOKEN must be used when GITHUB_TOKEN is empty, without consulting the gh CLI")
}

func TestResolveGithubCredentials_FallsBackToGhAuthToken(t *testing.T) {
	resetAuth(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	withStubGhAuthToken(t, "from-gh-cli", nil)

	resolveGithubCredentials(false)

	tok, _ := homebrew.AuthStateForTest()
	assert.Equal(t, "from-gh-cli", tok,
		"gh auth token output must be used when both env vars are empty")
}

func TestResolveGithubCredentials_NoTokenAnywhereLeavesUnauth(t *testing.T) {
	resetAuth(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	withStubGhAuthToken(t, "", errors.New("gh not installed"))

	resolveGithubCredentials(false)

	tok, disabled := homebrew.AuthStateForTest()
	assert.Empty(t, tok, "no token should be configured when nothing is available")
	assert.False(t, disabled, "auth must not be disabled implicitly — only --dont-use-token does that")
}

// TestGhAuthToken_GracefulWhenGhMissing is a live test against the real
// ghAuthToken (not a stub). With PATH pointed at an empty directory
// `gh` cannot be found — the function must surface an error, not panic,
// so resolveGithubCredentials can keep going and the program can still
// run against public repos without the gh CLI installed.
func TestGhAuthToken_GracefulWhenGhMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tok, err := ghAuthToken()
	require.Error(t, err, "gh CLI missing must produce an error, not a panic")
	assert.Empty(t, tok)
	assert.Contains(t, err.Error(), "gh CLI not installed",
		"error must name the specific failure mode so a user can diagnose it from logs")
}

// gh auth token returning empty stdout with no error is treated as "no
// token available," same as a non-zero exit, so we fall through to the
// no-token warning rather than configuring an empty bearer token.
func TestResolveGithubCredentials_IgnoresEmptyGhAuthTokenOutput(t *testing.T) {
	resetAuth(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	withStubGhAuthToken(t, "", nil)

	resolveGithubCredentials(false)

	tok, disabled := homebrew.AuthStateForTest()
	assert.Empty(t, tok)
	assert.False(t, disabled)
}
