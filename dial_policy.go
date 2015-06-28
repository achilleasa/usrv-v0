package usrv

import (
	"math/rand"
	"sync"
	"time"
)

// A dial policy is essentially a generator of time.Duration objects
// for dial retries. The generator should return an error if no
// more dial attempts should be made.
type DialPolicy interface {

	// Reset the attempt counter.
	ResetAttempts()

	// Get the current attempt.
	CurAttempt() uint32

	// Get a time.Duration value for scheduling a re-dial attempt.
	// An error will be returned if the max number of attempts has been exceeded.
	NextRetry() (time.Duration, error)
}

// A structure for implementing dial policies
type dialPolicyImpl struct {
	// A mutex for guarding changes to the struct fields.
	sync.Mutex

	curAttempt uint32

	retryGenerator func(curAttempt uint32) (time.Duration, error)
}

// Reset the attempt counter. Implements the DialPolicy interface.
func (d *dialPolicyImpl) ResetAttempts() {
	d.Lock()
	defer d.Unlock()

	d.curAttempt = 0
}

// Get the attempt counter. Implements the DialPolicy interface.
func (d *dialPolicyImpl) CurAttempt() uint32 {
	d.Lock()
	defer d.Unlock()

	return d.curAttempt
}

// Get the next retry interval. Implements the DialPolicy interface.
func (d *dialPolicyImpl) NextRetry() (time.Duration, error) {
	d.Lock()
	defer d.Unlock()

	d.curAttempt++
	return d.retryGenerator(d.curAttempt)
}

// Implements an periodic dial policy that returns
// the same time.Duration value between all attempts.
func PeriodicPolicy(maxAttempts uint32, retry time.Duration) *dialPolicyImpl {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	return &dialPolicyImpl{
		curAttempt: 0,
		retryGenerator: func(curAttempt uint32) (time.Duration, error) {
			if curAttempt > maxAttempts {
				return 0, ErrTimeout
			}

			return retry, nil
		},
	}
}

// Implements an exponential backoff dial policy that returns
// a random time.Duration between 0 and 2^attempt - 1 in the
// specified unit. Max attempts should be [1, 32]. Any
// value outside that range will be capped to the nearest limit.
func ExpBackoffPolicy(maxAttempts uint32, retryUnit time.Duration) *dialPolicyImpl {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if maxAttempts > 32 {
		maxAttempts = 32
	}

	return &dialPolicyImpl{
		curAttempt: 0,
		retryGenerator: func(curAttempt uint32) (time.Duration, error) {
			if curAttempt > maxAttempts {
				return 0, ErrTimeout
			}

			return time.Duration(rand.Int63n(1 << curAttempt)), nil
		},
	}
}
