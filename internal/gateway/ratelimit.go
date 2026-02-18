package gateway

import (
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	burst      float64
	rate       float64
	lastRefill time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
	}
}

func (l *RateLimiter) Allow(key string, rate float64) bool {
	if rate <= 0 {
		return false
	}
	burst := rate * 2
	if burst < 1 {
		burst = 1
	}

	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &tokenBucket{
			tokens:     burst,
			burst:      burst,
			rate:       rate,
			lastRefill: now,
		}
		l.buckets[key] = bucket
	}

	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.lastRefill = now
	if elapsed > 0 {
		bucket.tokens += elapsed * bucket.rate
		if bucket.tokens > bucket.burst {
			bucket.tokens = bucket.burst
		}
	}

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens -= 1
	return true
}

func (l *RateLimiter) Snapshot() map[string]float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]float64, len(l.buckets))
	for key, bucket := range l.buckets {
		out[key] = bucket.tokens
	}
	return out
}
