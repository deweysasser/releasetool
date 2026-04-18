package homebrew

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTransport returns a predetermined sequence of responses. When
// exhausted it errors so a test that expects N retries can't silently
// over-retry. It records requests it actually saw so we can verify the
// retry count.
type stubTransport struct {
	mu        sync.Mutex
	responses []*http.Response
	calls     int
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.responses) {
		return nil, fmt.Errorf("stubTransport exhausted after %d calls", s.calls)
	}
	resp := s.responses[s.calls]
	s.calls++
	if resp.Request == nil {
		resp.Request = req
	}
	return resp, nil
}

func (s *stubTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func mkResp(status int, headers map[string]string, body string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
}

// newTestTransport builds a retry transport that never actually sleeps
// and uses a fixed "now" so X-RateLimit-Reset waits are deterministic.
// Waits are recorded so tests can assert on them.
func newTestTransport(base http.RoundTripper, now time.Time) (*rateLimitRetryTransport, *[]time.Duration) {
	waits := []time.Duration{}
	t := &rateLimitRetryTransport{
		base:       base,
		maxRetries: defaultMaxRetries,
		minBackoff: 1 * time.Second,
		maxBackoff: 60 * time.Second,
		maxSleep:   60 * time.Minute,
		sleepFn: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			return ctx.Err()
		},
		nowFn:   func() time.Time { return now },
		onRetry: func(int, time.Duration, int) {},
	}
	return t, &waits
}

func doGet(t *testing.T, rt http.RoundTripper) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", "http://example.test/path", nil)
	require.NoError(t, err)
	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	return resp
}

func TestRetry_PassThroughOn200(t *testing.T) {
	stub := &stubTransport{responses: []*http.Response{mkResp(200, nil, "ok")}}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, stub.count(), "success must not trigger retry")
	assert.Empty(t, *waits)
}

func TestRetry_429WithRetryAfterDeltaSeconds(t *testing.T) {
	stub := &stubTransport{responses: []*http.Response{
		mkResp(429, map[string]string{"Retry-After": "7"}, ""),
		mkResp(200, nil, "ok"),
	}}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 2, stub.count())
	require.Len(t, *waits, 1)
	assert.Equal(t, 7*time.Second, (*waits)[0], "must honor Retry-After delta-seconds exactly")
}

func TestRetry_429WithRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 4, 17, 22, 0, 0, 0, time.UTC)
	when := now.Add(90 * time.Second)
	stub := &stubTransport{responses: []*http.Response{
		mkResp(429, map[string]string{"Retry-After": when.Format(http.TimeFormat)}, ""),
		mkResp(200, nil, "ok"),
	}}
	rt, waits := newTestTransport(stub, now)

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode)
	require.Len(t, *waits, 1)
	// HTTP-date formatting has second-level resolution.
	assert.InDelta(t, float64(90*time.Second), float64((*waits)[0]), float64(time.Second))
}

// oneShotReader lets the test count how many bytes the transport actually
// pulled from the body — used to prove the cap is honored even when the
// body is effectively unbounded (a hostile proxy returning a huge 403).
type oneShotReader struct {
	src  io.Reader
	read int
	mu   sync.Mutex
}

func (r *oneShotReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	r.mu.Lock()
	r.read += n
	r.mu.Unlock()
	return n, err
}

func (r *oneShotReader) Close() error { return nil }

// TestRetry_403BodyReadIsCapped proves that when the transport has to
// look at the body to decide whether a 403 is a rate-limit signal, it
// reads at most maxRateLimitBodyBytes. Without the cap, a misbehaving
// proxy returning a gigabyte body on 403 would OOM the tool on CI.
func TestRetry_403BodyReadIsCapped(t *testing.T) {
	// Simulate an effectively unbounded body: a reader that yields the
	// byte 'x' forever. The transport must stop reading at the cap.
	infinite := &oneShotReader{src: &infiniteByteReader{b: 'x'}}
	resp := &http.Response{
		StatusCode: 403,
		Status:     "403 Forbidden",
		Header:     http.Header{},
		Body:       infinite,
	}
	stub := &stubTransport{responses: []*http.Response{
		resp,
		mkResp(200, nil, "ok"), // retry target — we only care about the body read below
	}}
	rt, _ := newTestTransport(stub, time.Now())

	_ = doGet(t, rt)

	infinite.mu.Lock()
	got := infinite.read
	infinite.mu.Unlock()

	assert.LessOrEqual(t, got, maxRateLimitBodyBytes,
		"looksLikeRateLimit must bound its body read; read %d bytes (cap %d)",
		got, maxRateLimitBodyBytes)
}

type infiniteByteReader struct{ b byte }

func (r *infiniteByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

func TestRetry_403PrimaryRateLimitUsesResetHeader(t *testing.T) {
	now := time.Date(2026, 4, 17, 22, 0, 0, 0, time.UTC)
	reset := now.Add(41*time.Minute + 42*time.Second)

	stub := &stubTransport{responses: []*http.Response{
		mkResp(403, map[string]string{
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":     strconv.FormatInt(reset.Unix(), 10),
		}, `{"message":"API rate limit exceeded for 68.184.47.119."}`),
		mkResp(200, nil, "ok"),
	}}
	rt, waits := newTestTransport(stub, now)

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 2, stub.count())
	require.Len(t, *waits, 1)
	assert.InDelta(t, float64(41*time.Minute+42*time.Second), float64((*waits)[0]), float64(time.Second),
		"primary rate limit wait must derive from X-RateLimit-Reset when Retry-After is absent")
}

func TestRetry_403SecondaryRateLimitBodyOnly(t *testing.T) {
	// GitHub's documented secondary-limit body, with no rate-limit
	// headers of any kind. The body is our only signal.
	stub := &stubTransport{responses: []*http.Response{
		mkResp(403, nil, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again.","documentation_url":"https://docs.github.com/..."}`),
		mkResp(200, nil, "ok"),
	}}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode, "secondary rate limit must be detected from the body")
	require.Len(t, *waits, 1)
	assert.Equal(t, 1*time.Second, (*waits)[0], "no headers => minimum exp backoff on first retry")
}

func TestRetry_403LegacyAbuseDetectionBody(t *testing.T) {
	// Pre-2022 phrasing. Still seen in the wild against old endpoints.
	stub := &stubTransport{responses: []*http.Response{
		mkResp(403, nil, `{"message":"You have triggered an abuse detection mechanism. Please wait a few minutes."}`),
		mkResp(200, nil, "ok"),
	}}
	rt, _ := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode,
		"legacy 'abuse detection' 403 must still be retried even though it doesn't contain the phrase 'rate limit'")
}

func TestRetry_403WithoutRateLimitSignalsPassesThrough(t *testing.T) {
	body := `{"message":"Resource not accessible by integration"}`
	stub := &stubTransport{responses: []*http.Response{
		mkResp(403, nil, body),
	}}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 403, resp.StatusCode, "non-rate-limit 403 must pass through unchanged")
	assert.Equal(t, 1, stub.count(), "non-rate-limit 403 must not be retried")
	assert.Empty(t, *waits)

	// Body must still be readable by the caller — looksLikeRateLimit
	// consumed it while checking, so it must have rewrapped.
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(got), "body must survive rate-limit detection so go-github can parse the error message")
}

func TestRetry_ExponentialBackoffWhenNoHints(t *testing.T) {
	stub := &stubTransport{responses: []*http.Response{
		mkResp(429, nil, ""),
		mkResp(429, nil, ""),
		mkResp(429, nil, ""),
		mkResp(200, nil, "ok"),
	}}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode)
	require.Equal(t, []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}, *waits,
		"without Retry-After or X-RateLimit-Reset, waits must follow 2^n exponential backoff from minBackoff")
}

func TestRetry_ExhaustsAndReturnsLastResponse(t *testing.T) {
	responses := []*http.Response{}
	for i := 0; i < defaultMaxRetries+1; i++ {
		responses = append(responses, mkResp(429, nil, fmt.Sprintf("attempt %d", i)))
	}
	stub := &stubTransport{responses: responses}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 429, resp.StatusCode, "after exhausting retries we must return the last rate-limit response, not an error")
	assert.Equal(t, defaultMaxRetries+1, stub.count(), "total attempts = 1 initial + maxRetries")
	assert.Len(t, *waits, defaultMaxRetries, "must sleep between attempts, not after the last one")

	// Body must still be readable so the caller sees GitHub's message.
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(got), "attempt")
}

func TestRetry_ContextCancellationAbortsSleep(t *testing.T) {
	// Transport that would return a rate-limit response if called
	// twice, so we can verify the retry never happens when the
	// context is cancelled during the sleep.
	stub := &stubTransport{responses: []*http.Response{
		mkResp(429, map[string]string{"Retry-After": "3600"}, ""),
		mkResp(200, nil, "ok"),
	}}
	rt := &rateLimitRetryTransport{
		base:       stub,
		maxRetries: defaultMaxRetries,
		minBackoff: 1 * time.Second,
		maxBackoff: 60 * time.Second,
		maxSleep:   60 * time.Minute,
		sleepFn: func(ctx context.Context, d time.Duration) error {
			return context.Canceled
		},
		nowFn:   time.Now,
		onRetry: func(int, time.Duration, int) {},
	}

	req, err := http.NewRequest("GET", "http://example.test/x", nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	_, err = rt.RoundTrip(req)
	require.Error(t, err, "cancelled context during backoff must surface an error, not silently return stale response")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, stub.count(), "must not issue the retry after cancellation")
}

func TestRetry_CapsSleepAtMaxSleep(t *testing.T) {
	// Retry-After of 10 hours — absurd; the transport must cap it so a
	// misbehaving server can't strand the caller indefinitely.
	stub := &stubTransport{responses: []*http.Response{
		mkResp(429, map[string]string{"Retry-After": "36000"}, ""),
		mkResp(200, nil, "ok"),
	}}
	rt, waits := newTestTransport(stub, time.Now())

	resp := doGet(t, rt)
	assert.Equal(t, 200, resp.StatusCode)
	require.Len(t, *waits, 1)
	assert.Equal(t, 60*time.Minute, (*waits)[0], "wait must be capped at maxSleep when the server asks for more")
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 4, 17, 22, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   string
		want time.Duration
		ok   bool
	}{
		{"delta_zero", "0", 0, true},
		{"delta_positive", "42", 42 * time.Second, true},
		{"delta_negative_floored_to_zero", "-5", 0, true},
		{"http_date_future", now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true},
		{"http_date_past_floored_to_zero", now.Add(-10 * time.Second).Format(http.TimeFormat), 0, true},
		{"empty", "", 0, false},
		{"garbage", "tomorrow", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.in, now)
			assert.Equal(t, tc.ok, ok)
			if ok {
				// HTTP-date resolution is whole seconds; allow a 1s slop.
				assert.InDelta(t, float64(tc.want), float64(got), float64(time.Second))
			}
		})
	}
}

// TestRetry_StatusStringDetection covers the case where a 403 response
// carries "rate limit" in the status line itself — extremely rare in
// practice but the user's spec calls it out, so pin the behavior.
func TestRetry_StatusStringDetection(t *testing.T) {
	resp := mkResp(403, nil, `{"message":"Forbidden"}`)
	resp.Status = "403 API rate limit exceeded"
	stub := &stubTransport{responses: []*http.Response{
		resp,
		mkResp(200, nil, "ok"),
	}}
	rt, _ := newTestTransport(stub, time.Now())

	got := doGet(t, rt)
	assert.Equal(t, 200, got.StatusCode,
		"a status line mentioning 'rate limit' must trigger retry even when the body and headers don't")
}

// Compile-time sanity check that the transport satisfies http.RoundTripper.
var _ http.RoundTripper = (*rateLimitRetryTransport)(nil)

// The compiler would catch this anyway, but keeping the reference
// pins our public expectation: tests shouldn't accidentally change
// the signature.
var _ = errors.New
var _ = strings.Contains
