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

func newLeakyBucket(capacity int64, size int64) Bucket {

	return simpleLeakyBucket{
		capacity,
		newFixedIntervalRefillStrategy(capacity, time.Duration(1000*1000*1000)),
		microSleepStrategy{},
		size,
	}
}


// SimpleBucket
// Implementation of Bucket interface
type simpleLeakyBucket struct {
	capacity       int64
	refillStrategy RefillStrategy
	sleepStrategy  SleepStrategy
	size           int64
	sync.RWMutex
}

func (b *simpleLeakyBucket) Capacity() int64 {
	return b.capacity
}

func (b *simpleLeakyBucket) NumberOfTokens() int64 {

	b.RLock()
	defer b.RUnlock()

	b.refill()

	return b.size
}

func (b *simpleLeakyBucket) DurationUntilNextRefill() int64 {
	return b.refillStrategy.DurationUntilNextRefill()
}

func (b *simpleLeakyBucket) TryConsume() bool {
	return b.TryConsumeMulti(1)
}

func (b *simpleLeakyBucket) TryConsumeMulti(numTokens int64) bool {

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

func (b *simpleLeakyBucket) Consume() {
	b.ConsumeMulti(1)
}

func (b *simpleLeakyBucket) ConsumeMulti(numTokens int64) {

	for {
		if b.TryConsumeMulti(numTokens) {
			break
		}

		b.sleepStrategy.Sleep()
	}

}

func (b *simpleLeakyBucket) refill() {
	b.Lock()
	defer b.Unlock()

	newTokens := math.Min(b.capacity, math.Max(0, b.refillStrategy.Refill()))
	b.size = math.Max(0, math.Min(b.size + newTokens, b.capacity))
}

func newFixedIntervalRefillStrategy(numTokensPerPeriod int64, period time.Duration) *FixedIntervalRefillStrategy {

	return &FixedIntervalRefillStrategy{
		numTokensPerPeriod,
		period,
		-period,
		-period,
	}

}

//FixedIntervalRefillStrategy
//Implementation of RefillStrategy
type FixedIntervalRefillStrategy struct {
	numTokensPerPeriod          int64
	periodDurationInNanoseconds int64
	lastRefillTime              int64
	nextRefillTime              int64
}

func (f *FixedIntervalRefillStrategy) Refill() int64 {

	now := time.Now()

	if now < f.nextRefillTime {
		return 0
	}

	numPeriods := math.Max(0, (now - f.lastRefillTime) / f.periodDurationInNanoseconds)

	f.lastRefillTime += numPeriods * f.periodDurationInNanoseconds
	f.nextRefillTime = f.lastRefillTime + f.periodDurationInNanoseconds

	return numPeriods * f.numTokensPerPeriod

}

func (f *FixedIntervalRefillStrategy) DurationUntilNextRefill() time.Duration {

	now := time.Now()
	return math.Max(0, f.nextRefillTime - now)

}

type noSleepStrategy struct {

}

func (s *noSleepStrategy) Sleep() {
	return
}

type microSleepStrategy struct {

}

func (s *microSleepStrategy) Sleep() {
	// Sleep for the smallest unit of time possible just to relinquish control
	// and to allow other routines to receive cpu time.
	time.Sleep(1)
}

