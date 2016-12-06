package bus

import (
	"time"
	"sync"
	"math"
)

// Bucket
// Interface definition for a simple leaky bucket.
type Bucket interface {
	// Returns the capacity of this token bucket.  This is the maximum number of tokens that the bucket can hold at
	// any one time.
	Capacity() int64

	// Returns the current number of tokens in the bucket.  If the bucket is empty then this method will return 0.
	NumberOfTokens() int64

	// Returns the amount of time in the specified time unit until the next group of tokens can be added to the token
	// bucket.
	DurationUntilNextRefill() time.Duration

	// Attempts to consume a single token from the bucket.  If it was consumed then true is returned, otherwise
	// false is returned.
	TryConsume() bool

	// Attempts to consume provided number of tokens from the bucket.  If it was consumed then true is returned, otherwise
	// false is returned.
	TryConsumeMulti(numTokens int64) bool

	// Consumes a single token from the bucket.  If no token is currently available then this method will block until a
	// token becomes available.
	Consume()

	// Consumes provided amount of tokens from the bucket.  If no token is currently available then this method will block until necessary
	// tokens become available.
	ConsumeMulti(numToken int64)
}

type RefillStrategy interface {
	// Returns the number of tokens to be added to the token bucket.
	Refill() int64

	// Returns the remaining duration for the next refill to be allowed
	DurationUntilNextRefill() time.Duration
}

type SleepStrategy interface {
	// For blocking consume requests
	Sleep()
}

type DefaultBucket struct {
	capacity       int64
	refillStrategy RefillStrategy
	sleepStrategy  SleepStrategy
	size           int64
	sync.RWMutex
}

func (b *DefaultBucket) Capacity() int64 {
	return b.capacity
}

func (b *DefaultBucket) NumberOfTokens() int64 {

	b.RLock()
	defer b.RUnlock()

	b.refill()

	return b.size
}

func (b *DefaultBucket) DurationUntilNextRefill() int64 {
	return b.refillStrategy.DurationUntilNextRefill()
}

func (b *DefaultBucket) TryConsume() bool {
	return b.TryConsumeMulti(1)
}

func (b *DefaultBucket) TryConsumeMulti(numTokens int64) bool {

	b.RLock()
	defer b.RUnlock()

	if numTokens > b.capacity {
		numTokens = b.capacity
	}

	b.refill()

	if (numTokens <= b.size) {
		b.size -= numTokens
		return true
	}

	return false

}

func (b *DefaultBucket) Consume() {
	b.ConsumeMulti(1)
}

func (b *DefaultBucket) ConsumeMulti(numTokens int64) {

	for {
		if b.TryConsumeMulti(numTokens) {
			break
		}

		b.sleepStrategy.Sleep()
	}

}

func (b *DefaultBucket) refill() {
	b.Lock()
	defer b.Unlock()

	newTokens := math.Min(b.capacity, math.Max(0, b.refillStrategy.Refill()))
	b.size = math.Max(0, math.Min(b.size + newTokens, b.capacity))
}
