package limiter

import (
	"fmt"
	"sync"
	"time"

	"argus/internal/types"
)

// RateLimiter 限流器
type RateLimiter struct {
	mu       sync.RWMutex
	config   types.RateLimitConfig
	history  map[string][]time.Time // 操作类型 -> 时间戳列表
}

// NewRateLimiter 创建限流器
func NewRateLimiter(config types.RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		config:  config,
		history: make(map[string][]time.Time),
	}
}

// Check 检查是否超过限流
func (r *RateLimiter) Check(opType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 获取配置
	var limit types.RateLimit
	switch opType {
	case "exec":
		limit = r.config.Exec
	case "install":
		limit = r.config.Install
	case "write_file":
		limit = r.config.WriteFile
	case "read_file":
		limit = r.config.ReadFile
		if limit.MaxPerSecond == 0 && limit.MaxPerMinute == 0 && limit.MaxPerHour == 0 {
			limit = types.RateLimit{MaxPerSecond: 100, MaxPerMinute: 3000}
		}
	case "exec_command":
		limit = r.config.ExecCommand
		if limit.MaxPerSecond == 0 && limit.MaxPerMinute == 0 && limit.MaxPerHour == 0 {
			limit = types.RateLimit{MaxPerSecond: 10, MaxPerMinute: 200}
		}
	case "git_operation":
		limit = r.config.GitOperation
		if limit.MaxPerSecond == 0 && limit.MaxPerMinute == 0 && limit.MaxPerHour == 0 {
			limit = types.RateLimit{MaxPerSecond: 5, MaxPerMinute: 60}
		}
	default:
		return nil // 未知类型不限流
	}

	// 清理过期记录
	r.cleanup(opType)

	// 检查限流
	now := time.Now()
	timestamps := r.history[opType]

	// 每秒限制
	if limit.MaxPerSecond > 0 {
		count := r.countInWindow(timestamps, now.Add(-time.Second))
		if count >= limit.MaxPerSecond {
			return fmt.Errorf("rate limited: max %d per second", limit.MaxPerSecond)
		}
	}

	// 每分钟限制
	if limit.MaxPerMinute > 0 {
		count := r.countInWindow(timestamps, now.Add(-time.Minute))
		if count >= limit.MaxPerMinute {
			return fmt.Errorf("rate limited: max %d per minute", limit.MaxPerMinute)
		}
	}

	// 每小时限制
	if limit.MaxPerHour > 0 {
		count := r.countInWindow(timestamps, now.Add(-time.Hour))
		if count >= limit.MaxPerHour {
			return fmt.Errorf("rate limited: max %d per hour", limit.MaxPerHour)
		}
	}

	// 记录本次操作
	r.history[opType] = append(timestamps, now)
	return nil
}

// Record 记录一次操作（用于外部记录，不检查限流）
func (r *RateLimiter) Record(opType string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.history[opType] = append(r.history[opType], time.Now())
}

// cleanup 清理过期记录
func (r *RateLimiter) cleanup(opType string) {
	now := time.Now()
	cutoff := now.Add(-time.Hour) // 保留1小时内的记录

	timestamps := r.history[opType]
	var valid []time.Time
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	r.history[opType] = valid
}

// countInWindow 统计时间窗口内的操作数
func (r *RateLimiter) countInWindow(timestamps []time.Time, windowStart time.Time) int {
	count := 0
	for _, t := range timestamps {
		if t.After(windowStart) {
			count++
		}
	}
	return count
}

// GetStats 获取统计信息
func (r *RateLimiter) GetStats(opType string) map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	timestamps := r.history[opType]

	return map[string]int{
		"last_second":  r.countInWindow(timestamps, now.Add(-time.Second)),
		"last_minute":  r.countInWindow(timestamps, now.Add(-time.Minute)),
		"last_hour":    r.countInWindow(timestamps, now.Add(-time.Hour)),
		"total":        len(timestamps),
	}
}
