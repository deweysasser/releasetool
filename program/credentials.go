package program

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/deweysasser/releasetool/homebrew"
	"github.com/rs/zerolog/log"
)

// ghAuthToken shells out to the `gh` CLI to obtain a token that a
// locally-authenticated user already has on disk. It's a package
// variable so tests can substitute a non-exec implementation.
//
// It must never propagate a panic or blow up the program when the `gh`
// binary isn't installed — that's the common case on CI runners and
// on any machine that hasn't opted in to the `gh` CLI. Both the
// LookPath check and the cmd.Output() call return plain errors that
// resolveGithubCredentials treats as "no token from this source."
var ghAuthToken = func() (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not installed or not in PATH: %w", err)
	}
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token failed: %w", err)
	}
	t := strings.TrimSpace(string(out))
	if t == "" {
		return "", errors.New("gh auth token returned empty output")
	}
	return t, nil
}

// resolveGithubCredentials decides how subsequent GitHub API calls will
// authenticate and pushes that decision into the homebrew package. The
// order is:
//
//  1. --dont-use-token → all requests go out unauthenticated.
//  2. GITHUB_TOKEN env var.
//  3. GH_TOKEN env var (what the `gh` CLI sets).
//  4. `gh auth token` — use whatever token the user has already granted
//     to the `gh` CLI, so a `gh auth login` is enough to avoid the 60
//     req/hr/IP anonymous rate limit without any extra setup.
//  5. Nothing — warn loudly because the rate limit is almost certain to
//     bite on multi-repo runs.
func resolveGithubCredentials(disabled bool) {
	// Start from a clean slate so the resolver is idempotent — callers
	// can invoke it more than once (tests do) without worrying about
	// stale state from an earlier call leaking through.
	homebrew.SetToken("")
	homebrew.SetAuthDisabled(false)

	if disabled {
		homebrew.SetAuthDisabled(true)
		log.Info().Msg("--dont-use-token set; GitHub API requests will be unauthenticated (60 req/hr per IP)")
		return
	}

	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		homebrew.SetToken(t)
		log.Debug().Str("source", "GITHUB_TOKEN").Msg("resolved GitHub token")
		return
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		homebrew.SetToken(t)
		log.Debug().Str("source", "GH_TOKEN").Msg("resolved GitHub token")
		return
	}

	if t, err := ghAuthToken(); err == nil && t != "" {
		homebrew.SetToken(t)
		log.Debug().Str("source", "gh auth token").Msg("resolved GitHub token")
		return
	}

	log.Warn().Msg("No GitHub token found (GITHUB_TOKEN, GH_TOKEN, or `gh auth token`). Requests will be rate-limited to 60/hr per IP; run `gh auth login` or set GITHUB_TOKEN to avoid this.")
}
