package limiter

import (
	"fmt"
	"sync"
	"time"

	"argus/internal/types"
)

// CircuitBreakerState 熔断器状态
type CircuitBreakerState int

const (
	StateClosed    CircuitBreakerState = iota // 关闭（正常）
	StateOpen                                  // 开启（熔断）
	StateHalfOpen                              // 半开（试探）
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	mu              sync.RWMutex
	config          types.CircuitBreakerConfig
	states          map[string]*circuitState // 操作类型 -> 状态
}

// circuitState 单个熔断器状态
type circuitState struct {
	state           CircuitBreakerState
	failures        int
	lastFailureTime time.Time
	lastSuccessTime time.Time
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(config types.CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		states: make(map[string]*circuitState),
	}
}

// Check 检查是否允许执行
func (cb *CircuitBreaker) Check(opType string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.getState(opType)
	config := cb.getConfig(opType)

	// 检查是否处于熔断状态
	if state.state == StateOpen {
		// 检查是否超过超时时间
		if time.Since(state.lastFailureTime) > time.Duration(config.TimeoutSeconds)*time.Second {
			// 切换到半开状态
			state.state = StateHalfOpen
			state.failures = 0
			return nil
		}
		return fmt.Errorf("circuit breaker open: please wait %d seconds", 
			config.TimeoutSeconds-int(time.Since(state.lastFailureTime).Seconds()))
	}

	return nil
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess(opType string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.getState(opType)
	state.failures = 0
	state.lastSuccessTime = time.Now()

	// 如果是半开状态，切换到关闭
	if state.state == StateHalfOpen {
		state.state = StateClosed
	}
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure(opType string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.getState(opType)
	config := cb.getConfig(opType)

	state.failures++
	state.lastFailureTime = time.Now()

	// 如果失败次数超过阈值，切换到开启状态
	if state.failures >= config.FailureThreshold {
		state.state = StateOpen
	}
}

// getState 获取状态（需要加锁后调用）
func (cb *CircuitBreaker) getState(opType string) *circuitState {
	if state, ok := cb.states[opType]; ok {
		return state
	}
	// 创建新状态
	state := &circuitState{
		state: StateClosed,
	}
	cb.states[opType] = state
	return state
}

// getConfig 获取配置
func (cb *CircuitBreaker) getConfig(opType string) types.CircuitBreaker {
	switch opType {
	case "exec":
		return cb.config.Exec
	case "install":
		return cb.config.Install
	default:
		// 默认配置
		return types.CircuitBreaker{
			FailureThreshold: 5,
			TimeoutSeconds:   60,
		}
	}
}

// GetState 获取熔断器状态
func (cb *CircuitBreaker) GetState(opType string) string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state := cb.getState(opType)
	return state.state.String()
}

// GetStats 获取统计信息
func (cb *CircuitBreaker) GetStats(opType string) map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state := cb.getState(opType)
	config := cb.getConfig(opType)

	return map[string]interface{}{
		"state":             state.state.String(),
		"failures":          state.failures,
		"failure_threshold": config.FailureThreshold,
		"timeout_seconds":   config.TimeoutSeconds,
		"last_failure":      state.lastFailureTime,
		"last_success":      state.lastSuccessTime,
	}
}
