package tlimiter

import (
	"time"
)

// Rate defines the maximum number of events allowed for a key during a window
// of length [Rate.Period].
type Rate struct {
	// Period is the duration of each rate-limit window.
	Period time.Duration
	// Limit is the maximum event count allowed within Period.
	Limit int64
}

// NewRate returns a [Rate] configured with the supplied period and limit.
func NewRate(period time.Duration, limit int64) *Rate {
	return &Rate{
		Period: period,
		Limit:  limit,
	}
}
