package watchdog

import (
	"context"
	"errors"
	"sync"
	"time"
)

// oncePer is a very dumb rate limiter to protect watchdogs from abuse.
// It becomes pretty useless as replicas expand.
// There is a fancier version in branch fancyonceper,
// but another option is to just hide watchdog calls from public.
type oncePer struct {
	mu     sync.Mutex
	last   time.Time
	period time.Duration
	dog    watchdog
}

func NewOncePer(period time.Duration, dog watchdog) oncePer {
	return oncePer{
		period: period,
		dog:    dog,
	}
}

var errTooSoon = errors.New("too soon to call this again")

func (o *oncePer) Watchdog(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.last.IsZero() && time.Since(o.last) < o.period {
		return errTooSoon
	}

	o.last = time.Now()
	return o.dog.Watchdog(ctx) // allow more tries if this fails?
}
