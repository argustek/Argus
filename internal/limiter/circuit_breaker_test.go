package limiter

import (
	"testing"
	"time"

	"argus/internal/types"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_InitialState_Closed(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 3, TimeoutSeconds: 2},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	state := cb.GetState("exec")
	assert.Equal(t, "closed", state)
}

func TestCircuitBreaker_Check_Closed_Allows(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 3, TimeoutSeconds: 2},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	err := cb.Check("exec")
	assert.NoError(t, err)
}

func TestCircuitBreaker_Failures_TriggersOpen(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 3, TimeoutSeconds: 2},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	for i := 0; i < 3; i++ {
		cb.RecordFailure("exec")
	}

	state := cb.GetState("exec")
	assert.Equal(t, "open", state)
}

func TestCircuitBreaker_Open_RejectsRequests(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 2, TimeoutSeconds: 5},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	cb.RecordFailure("exec")
	cb.RecordFailure("exec")

	err := cb.Check("exec")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker open")
}

func TestCircuitBreaker_Open_To_HalfOpen_AfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 2, TimeoutSeconds: 1},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	cb.RecordFailure("exec")
	cb.RecordFailure("exec")
	assert.Equal(t, "open", cb.GetState("exec"))

	time.Sleep(1100 * time.Millisecond)

	err := cb.Check("exec")
	assert.NoError(t, err, "should transition to half-open after timeout")
	assert.Equal(t, "half-open", cb.GetState("exec"))
}

func TestCircuitBreaker_HalfOpen_Success_ReturnsToClosed(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 2, TimeoutSeconds: 1},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	cb.RecordFailure("exec")
	cb.RecordFailure("exec")
	time.Sleep(1100 * time.Millisecond)
	cb.Check("exec")

	cb.RecordSuccess("exec")
	assert.Equal(t, "closed", cb.GetState("exec"))
}

func TestCircuitBreaker_Success_ResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 30},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 30},
	})

	cb.RecordFailure("exec")
	cb.RecordFailure("exec")
	cb.RecordSuccess("exec")

	stats := cb.GetStats("exec")
	assert.Equal(t, 0, stats["failures"])
	assert.Equal(t, "closed", stats["state"])
}

func TestCircuitBreaker_SeparateStates_PerOpType(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 2, TimeoutSeconds: 5},
		Install: types.CircuitBreaker{FailureThreshold: 5, TimeoutSeconds: 10},
	})

	cb.RecordFailure("exec")
	cb.RecordFailure("exec")

	assert.Equal(t, "open", cb.GetState("exec"))
	assert.Equal(t, "closed", cb.GetState("install"))
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{
		Exec:    types.CircuitBreaker{FailureThreshold: 2, TimeoutSeconds: 60},
		Install: types.CircuitBreaker{FailureThreshold: 3, TimeoutSeconds: 30},
	})

	cb.RecordFailure("exec")
	cb.RecordFailure("exec")

	stats := cb.GetStats("exec")
	assert.NotNil(t, stats)
	assert.Equal(t, "open", stats["state"])
	assert.Equal(t, 2, stats["failures"])
}

func TestCircuitBreaker_DefaultConfig_ForUnknownOpType(t *testing.T) {
	cb := NewCircuitBreaker(types.CircuitBreakerConfig{})

	for i := 0; i < 5; i++ {
		cb.RecordFailure("unknown_op")
	}

	assert.Equal(t, "open", cb.GetState("unknown_op"))
}
