package homebrew

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	defaultMaxRetries = 3
	defaultMinBackoff = 1 * time.Second
	defaultMaxBackoff = 60 * time.Second
	// defaultMaxSleep caps any single wait so a misbehaving server can't
	// park the tool forever. 60 minutes is enough to ride out GitHub's
	// primary rate-limit reset window (60/hr resets on the hour), which
	// is the longest wait we realistically expect to honor.
	defaultMaxSleep = 60 * time.Minute
)

// rateLimitRetryTransport is an http.RoundTripper that transparently
// retries a request when the server reports a rate-limit hit:
//
//   - any 429 Too Many Requests, and
//   - a 403 Forbidden whose status line or body mentions "rate limit",
//     or that carries GitHub's X-RateLimit-Remaining: 0 header.
//
// Wait durations come from Retry-After first (delta-seconds or
// HTTP-date), then X-RateLimit-Reset, then exponential backoff from
// minBackoff doubling up to maxBackoff. Non-retry responses pass
// through unchanged with their body intact.
type rateLimitRetryTransport struct {
	base       http.RoundTripper
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration
	maxSleep   time.Duration

	// sleepFn and nowFn are test hooks so unit tests can run without
	// wall-clock waits or nondeterminism. onRetry lets a test capture
	// the attempt count and wait the transport chose.
	sleepFn func(context.Context, time.Duration) error
	nowFn   func() time.Time
	onRetry func(attempt int, wait time.Duration, status int)
}

func newRateLimitRetryTransport(base http.RoundTripper) *rateLimitRetryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &rateLimitRetryTransport{
		base:       base,
		maxRetries: defaultMaxRetries,
		minBackoff: defaultMinBackoff,
		maxBackoff: defaultMaxBackoff,
		maxSleep:   defaultMaxSleep,
		sleepFn:    ctxSleep,
		nowFn:      time.Now,
		onRetry:    defaultRetryLogger,
	}
}

// ctxSleep is a context-aware time.Sleep — waking either when the timer
// fires or when the request context is cancelled. Honoring cancellation
// matters because a server-indicated wait can legitimately be tens of
// minutes; Ctrl-C must still interrupt.
func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func defaultRetryLogger(attempt int, wait time.Duration, status int) {
	log.Warn().
		Int("attempt", attempt).
		Dur("wait", wait).
		Int("status", status).
		Msg("GitHub rate limited; backing off before retry")
}

func (t *rateLimitRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	var resp *http.Response
	var err error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		resp, err = t.base.RoundTrip(req)
		if err != nil {
			return resp, err
		}

		wait, retry := t.decide(resp, attempt)
		if !retry || attempt == t.maxRetries {
			return resp, nil
		}

		// Drain and close so the connection can be reused for the
		// retry; do this before sleeping so the socket isn't held
		// open across a potentially multi-minute wait.
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		t.onRetry(attempt+1, wait, resp.StatusCode)

		if err := t.sleepFn(ctx, wait); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func (t *rateLimitRetryTransport) decide(resp *http.Response, attempt int) (time.Duration, bool) {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return t.computeWait(resp, attempt), true
	case http.StatusForbidden:
		if t.looksLikeRateLimit(resp) {
			return t.computeWait(resp, attempt), true
		}
	}
	return 0, false
}

// looksLikeRateLimit reports whether a 403 response is GitHub's way of
// signaling a rate-limit hit. GitHub uses 403 (not 429) for both kinds
// of rate limit, so the status code alone is not enough:
//
//   - Primary rate limit: the per-hour request budget (60 anon / 5000
//     authed). Signaled by X-RateLimit-Remaining: 0, with the bucket's
//     reset time in X-RateLimit-Reset (UNIX seconds).
//
//   - Secondary rate limit: triggered by heavy or abusive traffic
//     patterns (search, rapid mutations). Usually carries a Retry-After
//     header and a JSON body with "You have exceeded a secondary rate
//     limit" or, on older responses, "abuse detection mechanism".
//
// Header checks run first because they're cheap and unambiguous on
// primary hits; the body is only buffered when headers don't settle
// it. Buffering requires rewrapping resp.Body so downstream readers
// (go-github's CheckResponse, our asset-download code) still see the
// original bytes if we end up not retrying.
func (t *rateLimitRetryTransport) looksLikeRateLimit(resp *http.Response) bool {
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	if resp.Header.Get("Retry-After") != "" {
		return true
	}
	if strings.Contains(strings.ToLower(resp.Status), "rate limit") {
		return true
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		resp.Body = http.NoBody
		return false
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	lower := strings.ToLower(string(body))
	// "rate limit" catches both primary and the modern secondary
	// phrasing; "abuse" catches pre-2022 secondary-limit responses.
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "abuse")
}

func (t *rateLimitRetryTransport) computeWait(resp *http.Response, attempt int) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if d, ok := parseRetryAfter(v, t.nowFn()); ok {
			return capWait(d, t.maxSleep)
		}
	}
	if r := resp.Header.Get("X-RateLimit-Reset"); r != "" {
		if reset, err := strconv.ParseInt(r, 10, 64); err == nil {
			d := time.Unix(reset, 0).Sub(t.nowFn())
			if d > 0 {
				return capWait(d, t.maxSleep)
			}
		}
	}
	return t.expBackoff(attempt)
}

// parseRetryAfter parses an HTTP Retry-After value, which per RFC 7231
// can be either delta-seconds (a non-negative integer) or an HTTP-date.
// Negative deltas and past dates are normalized to zero.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0, true
		}
		return time.Duration(n) * time.Second, true
	}
	if tm, err := http.ParseTime(v); err == nil {
		d := tm.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

func capWait(d, max time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > max {
		return max
	}
	return d
}

func (t *rateLimitRetryTransport) expBackoff(attempt int) time.Duration {
	shift := attempt
	if shift > 6 {
		shift = 6
	}
	d := t.minBackoff << shift
	if d > t.maxBackoff {
		d = t.maxBackoff
	}
	return d
}
