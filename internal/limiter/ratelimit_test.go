package limiter

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"argus/internal/types"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_Check_Basic(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{
		Exec:      types.RateLimit{MaxPerSecond: 100, MaxPerMinute: 200},
		Install:   types.RateLimit{MaxPerSecond: 50, MaxPerMinute: 100},
		WriteFile: types.RateLimit{MaxPerSecond: 30, MaxPerMinute: 60},
	})

	for i := 0; i < 50; i++ {
		err := rl.Check("exec")
		assert.NoError(t, err, "request %d should be allowed", i)
	}
}

func TestRateLimiter_Deny_WhenExhausted(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{
		Exec: types.RateLimit{MaxPerSecond: 2, MaxPerMinute: 2},
	})

	err1 := rl.Check("exec")
	err2 := rl.Check("exec")
	err3 := rl.Check("exec")

	assert.NoError(t, err1, "first request allowed")
	assert.NoError(t, err2, "second request allowed")
	assert.Error(t, err3, "third request should be denied")
	assert.Contains(t, err3.Error(), "rate limited")
}

func TestRateLimiter_Refill_AfterWait(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{
		Exec: types.RateLimit{MaxPerSecond: 1, MaxPerMinute: 60},
	})

	err1 := rl.Check("exec")
	assert.NoError(t, err1)

	err2 := rl.Check("exec")
	assert.Error(t, err2)

	time.Sleep(1100 * time.Millisecond)

	err3 := rl.Check("exec")
	assert.NoError(t, err3, "should allow after refill period")
}

func TestRateLimiter_ConcurrentSafe(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{
		Exec: types.RateLimit{MaxPerSecond: 500, MaxPerMinute: 30000},
	})

	var successCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rl.Check("exec"); err == nil {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Greater(t, successCount.Load(), int64(190), "most requests should succeed under high limit")
}

func TestRateLimiter_GetStats(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{
		Exec: types.RateLimit{MaxPerSecond: 10, MaxPerMinute: 100},
	})

	rl.Check("exec")

	stats := rl.GetStats("exec")
	assert.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats["total"], 1)
}

func TestRateLimiter_Record_DoesNotCheck(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{
		Exec: types.RateLimit{MaxPerSecond: 1, MaxPerMinute: 5},
	})

	for i := 0; i < 10; i++ {
		rl.Record("exec")
	}

	stats := rl.GetStats("exec")
	assert.Equal(t, 10, stats["total"], "Record should not check limits")
}

func TestRateLimiter_UnknownType_NotLimited(t *testing.T) {
	rl := NewRateLimiter(types.RateLimitConfig{})

	for i := 0; i < 100; i++ {
		err := rl.Check("unknown_type")
		assert.NoError(t, err, "unknown type should not be limited")
	}
}
