package tlimiter

import (
	"time"
)

// Rate defines the maximum number of events (Limit) allowed per window of length Period for each key.
type Rate struct {
	// Period is the duration of each rate-limit window.
	Period time.Duration
	// Limit is the maximum event count allowed within Period.
	Limit int64
}

// NewRate returns a pointer to a [Rate] with the given period and limit.
func NewRate(period time.Duration, limit int64) *Rate {
	return &Rate{
		Period: period,
		Limit:  limit,
	}
}
