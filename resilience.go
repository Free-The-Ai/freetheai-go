package freetheai

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var keyPattern = regexp.MustCompile(`^ftai_[0-9a-fA-F]{48}$`)

const (
	// limitRelax is how long a lowered concurrency limit holds before the client tries more again.
	limitRelax = 10 * time.Minute
	// maxBusyWait caps Retry-After so a call never hangs for minutes.
	maxBusyWait = 30 * time.Second
	// concurrencyRetries is the number of tries after a concurrency refusal.
	concurrencyRetries = 6
)

// sleepFunc waits between retries. Tests replace it so they run instantly.
var sleepFunc = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sleepFor(ctx context.Context, d time.Duration) error { return sleepFunc(ctx, d) }

type action int

const (
	actionRetry action = iota
	actionFallback
	actionRaise
)

type decision struct {
	action action
	delay  time.Duration
}

// decide says what to do after err on the try-th try (counting from 0) of one model.
func decide(err error, tries, maxRetries int, hasFallback bool) decision {
	raise := decision{action: actionRaise}
	fallbackOrRaise := func() decision {
		if hasFallback {
			return decision{action: actionFallback}
		}
		return raise
	}

	var limited *ConcurrencyLimitError
	if errors.As(err, &limited) {
		if tries < maxInt(maxRetries, concurrencyRetries) {
			return decision{action: actionRetry, delay: backoff(tries, 1, 10)}
		}
		return raise
	}
	var filtered *ContentFilterError
	var images *ImagesNotSupportedError
	var budget *ProviderBudgetError
	var notFound *ModelNotFoundError
	if errors.As(err, &filtered) || errors.As(err, &images) || errors.As(err, &budget) || errors.As(err, &notFound) {
		return fallbackOrRaise()
	}
	var busy *ModelBusyError
	if errors.As(err, &busy) {
		if hasFallback {
			return decision{action: actionFallback}
		}
		if tries < maxRetries {
			wait := backoff(tries, 5, 30)
			if busy.RetryAfter > 0 {
				wait = busy.RetryAfter
			}
			return decision{action: actionRetry, delay: minDuration(wait, maxBusyWait)}
		}
		return raise
	}

	var rateLimited *RateLimitError
	var apiErr *APIError
	var connErr *ConnectionError
	var interrupted *StreamInterruptedError
	var internal *InternalServerError
	retryable := errors.As(err, &connErr) || errors.As(err, &interrupted) || errors.As(err, &internal)
	if errors.As(err, &rateLimited) {
		retryable = true // only a plain RateLimitError; the specific 429s are handled above or are final
	}
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 408 || apiErr.StatusCode == 409) {
		retryable = true
	}
	if retryable {
		if tries < maxRetries {
			var retryAfter time.Duration
			if errors.As(err, &apiErr) {
				retryAfter = apiErr.RetryAfter
			}
			if retryAfter > 0 {
				return decision{action: actionRetry, delay: minDuration(retryAfter, maxBusyWait)}
			}
			return decision{action: actionRetry, delay: backoff(tries, 0.5, 8)}
		}
		return fallbackOrRaise()
	}
	return raise
}

// backoff returns an exponential wait with a little jitter.
func backoff(attempt int, baseSeconds, capSeconds float64) time.Duration {
	seconds := math.Min(baseSeconds*math.Pow(2, float64(attempt)), capSeconds)
	jitter := rand.Float64() * 0.25
	return time.Duration((seconds + jitter) * float64(time.Second))
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// gate limits the replies in flight to what the account allows.
//
// It starts open (or at a fixed cap). After a concurrency refusal it lowers the limit to the
// replies running alongside, and it opens again after limitRelax without another refusal.
type gate struct {
	mu        sync.Mutex
	fixed     int
	learned   int
	loweredAt time.Time
	inFlight  int
	changed   chan struct{}
}

func newGate(fixed int) *gate {
	return &gate{fixed: fixed, changed: make(chan struct{})}
}

// limitLocked returns the current limit, or 0 for no limit. The caller holds mu.
func (g *gate) limitLocked(now time.Time) int {
	if g.learned > 0 && now.Sub(g.loweredAt) > limitRelax {
		g.learned = 0
	}
	limit := 0
	for _, value := range []int{g.fixed, g.learned} {
		if value > 0 && (limit == 0 || value < limit) {
			limit = value
		}
	}
	return limit
}

// acquire waits for a free slot or until ctx ends.
func (g *gate) acquire(ctx context.Context) error {
	for {
		g.mu.Lock()
		limit := g.limitLocked(time.Now())
		if limit == 0 || g.inFlight < limit {
			g.inFlight++
			g.mu.Unlock()
			return nil
		}
		changed := g.changed
		g.mu.Unlock()

		timer := time.NewTimer(time.Second) // re-check at least once a second
		select {
		case <-changed:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		timer.Stop()
	}
}

// release frees a slot and wakes the waiters.
func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight > 0 {
		g.inFlight--
	}
	close(g.changed)
	g.changed = make(chan struct{})
}

// refused lowers the limit after a concurrency refusal. Call it while still holding a slot.
func (g *gate) refused() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.learned = maxInt(1, g.inFlight-1)
	g.loweredAt = time.Now()
}

// inFlightCount reports the slots taken (used by tests).
func (g *gate) inFlightCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inFlight
}

// suggestModels returns close model IDs for an unknown one, best first (at most 5).
func suggestModels(model string, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	var exact []string
	for _, id := range ids {
		if id == "fta/"+model || strings.HasSuffix(id, "/"+model) {
			exact = append(exact, id)
		}
	}
	if len(exact) > 0 {
		return firstN(exact, 5)
	}
	near := closeMatches(model, ids, 5)
	tails := map[string]string{}
	var tailNames []string
	for _, id := range ids {
		tail := id[strings.LastIndex(id, "/")+1:]
		if _, seen := tails[tail]; !seen {
			tailNames = append(tailNames, tail)
		}
		tails[tail] = id
	}
	tailModel := model[strings.LastIndex(model, "/")+1:]
	for _, tail := range closeMatches(tailModel, tailNames, 5) {
		if id := tails[tail]; !contains(near, id) {
			near = append(near, id)
		}
	}
	return firstN(near, 5)
}

// fixable returns "fta/"+model when that ID is among the suggestions and the model lacks a prefix.
func fixable(model string, suggestions []string) string {
	if strings.HasPrefix(model, "fta/") || strings.HasPrefix(model, "freetheai/") {
		return ""
	}
	if contains(suggestions, "fta/"+model) {
		return "fta/" + model
	}
	return ""
}

// closeMatches mirrors difflib.get_close_matches: ratio >= cutoff, best first, ties by name (descending).
func closeMatches(word string, options []string, n int) []string {
	type scored struct {
		text  string
		score float64
	}
	var hits []scored
	for _, option := range options {
		if score := similarity(word, option); score >= 0.6 {
			hits = append(hits, scored{option, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].text > hits[j].text
	})
	out := make([]string, 0, n)
	for _, hit := range hits {
		out = append(out, hit.text)
	}
	return firstN(out, n)
}

// similarity is the Ratcliff/Obershelp ratio in [0, 1], as Python's difflib computes it.
func similarity(a, b string) float64 {
	total := len(a) + len(b)
	if total == 0 {
		return 1
	}
	return 2 * float64(matchCount(a, 0, len(a), b, 0, len(b))) / float64(total)
}

func matchCount(a string, alo, ahi int, b string, blo, bhi int) int {
	if alo >= ahi || blo >= bhi {
		return 0
	}
	bestSize, bestI, bestJ := 0, alo, blo
	for i := alo; i < ahi; i++ {
		for j := blo; j < bhi; j++ {
			k := 0
			for i+k < ahi && j+k < bhi && a[i+k] == b[j+k] {
				k++
			}
			if k > bestSize {
				bestSize, bestI, bestJ = k, i, j
			}
		}
	}
	if bestSize == 0 {
		return 0
	}
	return bestSize +
		matchCount(a, alo, bestI, b, blo, bestJ) +
		matchCount(a, bestI+bestSize, ahi, b, bestJ+bestSize, bhi)
}

func firstN(values []string, n int) []string {
	if len(values) > n {
		return values[:n]
	}
	return values
}
