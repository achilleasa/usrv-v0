package usrv

import (
	"testing"
	"time"
)

func TestPeriodicPolicy(t *testing.T) {
	var maxAttempts uint32 = 10
	var attempt uint32
	period := time.Second * 5
	policy := PeriodicPolicy(maxAttempts, period)

	for attempt = 0; attempt < maxAttempts; attempt++ {
		next, err := policy.NextRetry()
		if err != nil {
			t.Fatalf("Expected to get the next attempt duration; got error %v", err)
		}

		if next != period {
			t.Fatalf("Expected to get a next attempt duration equal to %d; got %d", period, next)
		}
	}

	// The next attempt should fail
	_, err := policy.NextRetry()
	if err == nil {
		t.Fatalf("Expected to fail after exceeding maxAttempts=%d", maxAttempts)
	}

	// Test reset
	attempt = policy.CurAttempt()
	if attempt != maxAttempts+1 {
		t.Fatalf("Expected CurAttempt() to return %d; got %d", maxAttempts+1, attempt)
	}
	policy.ResetAttempts()
	_, err = policy.NextRetry()
	if err != nil {
		t.Fatalf("Expected NextRetry() to work after ResetAttempts(); failed with %v", err)
	}
}

func TestPeriodicPolicyLimits(t *testing.T) {
	policy := PeriodicPolicy(0, time.Second)

	var attempt uint32
	for attempt = 0; attempt < 1; attempt++ {
		_, err := policy.NextRetry()
		if err != nil {
			t.Fatalf("Expected to get the next attempt duration; got error %v", err)
		}
	}

	// The next attempt should fail
	_, err := policy.NextRetry()
	if err == nil {
		t.Fatalf("Expected to fail after exceeding maxAttempts=%d", 1)
	}
}

func TestExpBackoffPolicy(t *testing.T) {
	var maxAttempts uint32 = 10
	var attempt uint32
	retryUnit := time.Millisecond
	policy := ExpBackoffPolicy(maxAttempts, retryUnit)

	for attempt = 0; attempt < maxAttempts; attempt++ {
		next, err := policy.NextRetry()
		if err != nil {
			t.Fatalf("Expected to get the next attempt duration; got error %v", err)
		}

		limit := time.Millisecond * 1 << (attempt + 1)
		if next < 0 || next > limit {
			t.Fatalf("Expected to get a next attempt duration in the range (0, %d]; got %d", limit, next)
		}
	}

	// The next attempt should fail
	_, err := policy.NextRetry()
	if err == nil {
		t.Fatalf("Expected to fail after exceeding maxAttempts=%d", maxAttempts)
	}

}

func TestExpBackoffPolicyLimits(t *testing.T) {
	policy := ExpBackoffPolicy(0, time.Second)

	var attempt uint32
	for attempt = 0; attempt < 1; attempt++ {
		_, err := policy.NextRetry()
		if err != nil {
			t.Fatalf("Expected to get the next attempt duration; got error %v", err)
		}
	}

	// The next attempt should fail
	_, err := policy.NextRetry()
	if err == nil {
		t.Fatalf("Expected to fail after exceeding maxAttempts=%d", 1)
	}

	// Try upper limit
	policy = ExpBackoffPolicy(40, time.Second)

	for attempt = 0; attempt < 32; attempt++ {
		_, err := policy.NextRetry()
		if err != nil {
			t.Fatalf("Expected to get the next attempt duration; got error %v", err)
		}
	}

	// The next attempt should fail
	_, err = policy.NextRetry()
	if err == nil {
		t.Fatalf("Expected to fail after exceeding maxAttempts=%d", 32)
	}
}
