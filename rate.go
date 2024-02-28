/**
 * @Author: lidonglin
 * @Description:
 * @File:  rate.go
 * @Version: 1.0.0
 * @Date: 2024/02/28 10:36
 */

package tlimiter

import (
	"time"
)

type Rate struct {
	Period time.Duration
	Limit  int64
}

func NewRate(period time.Duration, limit int64) *Rate {
	return &Rate{
		Period: period,
		Limit:  limit,
	}
}
