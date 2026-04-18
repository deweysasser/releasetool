package timing

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectLog swaps zerolog's global Logger to a buffer-backed debug-level
// logger for the duration of a test and returns a func to fetch decoded
// JSON events on demand. Cleanup restores the original logger.
func redirectLog(t *testing.T) (decode func() []map[string]any) {
	t.Helper()
	var (
		buf bytes.Buffer
		mu  sync.Mutex
	)
	orig := log.Logger
	log.Logger = zerolog.New(&lockedWriter{w: &buf, mu: &mu}).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = orig })

	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		var events []map[string]any
		for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var ev map[string]any
			require.NoError(t, json.Unmarshal(line, &ev))
			events = append(events, ev)
		}
		return events
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestTimer_Done_EmitsStartEndAndElapsed(t *testing.T) {
	events := redirectLog(t)

	tmr := Start("unit-op")
	time.Sleep(5 * time.Millisecond)
	tmr.Done()

	evs := events()
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "unit-op", ev["op"])
	assert.Equal(t, "timing", ev["message"])
	assert.Equal(t, "debug", ev["level"])
	assert.NotEmpty(t, ev["start"], "start timestamp must be present")
	assert.NotEmpty(t, ev["end"], "end timestamp must be present")
	// zerolog encodes Dur as milliseconds (float64). A 5ms sleep can resolve
	// slightly low on some kernels; allow a small epsilon.
	elapsed, ok := ev["elapsed"].(float64)
	require.True(t, ok, "elapsed must be numeric; got %T (%v)", ev["elapsed"], ev["elapsed"])
	assert.GreaterOrEqual(t, elapsed, 4.0, "elapsed should be at least ~5ms")
}

func TestTimer_UsableFromDefer(t *testing.T) {
	events := redirectLog(t)

	func() {
		defer Start("deferred-op").Done()
		time.Sleep(time.Millisecond)
	}()

	evs := events()
	require.Len(t, evs, 1)
	assert.Equal(t, "deferred-op", evs[0]["op"])
}

func TestTimer_GoroutineSafe(t *testing.T) {
	events := redirectLog(t)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			defer Start("concurrent-op").Done()
		}()
	}
	wg.Wait()

	evs := events()
	assert.Len(t, evs, n, "every goroutine must emit exactly one event")
}
