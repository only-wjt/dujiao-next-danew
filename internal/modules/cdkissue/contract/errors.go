package contract

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidInput       = errors.New("cdk issue invalid input")
	ErrNotFound           = errors.New("cdk issue not found")
	ErrNotConfigured      = errors.New("cdk issue provider not configured")
	ErrRetryNotAllowed    = errors.New("cdk issue retry not allowed")
	ErrDisableNotAllowed  = errors.New("cdk issue disable not allowed")
	ErrQueueUnavailable   = errors.New("cdk issue queue unavailable")
	ErrBindingInvalid     = errors.New("cdk issue binding invalid")
	ErrConfigInvalid      = errors.New("cdk issue config invalid")
	ErrNotOrderable       = errors.New("cdk issue sku not orderable")
	ErrIssueRetryable     = errors.New("cdk issue retryable failure")
	ErrIssueHalted        = errors.New("cdk issue halted, waiting for admin")
	ErrIssueCodesMismatch = errors.New("cdk issue returned code count mismatch")
)

// GatewayError 是卡台返回的业务或协议错误。
type GatewayError struct {
	HTTPStatus int
	ErrorCode  string
	Message    string
	// Transient 为真表示结果未定，必须用同一幂等键重试；为假表示卡台明确拒绝。
	Transient bool
}

func (e *GatewayError) Error() string {
	if e == nil {
		return ""
	}
	if e.ErrorCode != "" {
		return fmt.Sprintf("zovocard http %d %s: %s", e.HTTPStatus, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("zovocard http %d: %s", e.HTTPStatus, e.Message)
}

// ClassifyHTTPStatus 按文档区分可重试与终态：409/429/5xx 结果待定，其余 4xx 为明确拒绝。
func ClassifyHTTPStatus(status int) (transient bool) {
	switch {
	case status == 409, status == 429:
		return true
	case status >= 500:
		return true
	case status >= 400:
		return false
	default:
		return true
	}
}

// BindingError 携带面向管理员的校验说明。
type BindingError struct {
	Reason string
}

func (e *BindingError) Error() string { return e.Reason }

func (e *BindingError) Unwrap() error { return ErrBindingInvalid }

// ConfigError 携带面向管理员的配置校验说明。
type ConfigError struct {
	Reason string
}

func (e *ConfigError) Error() string { return e.Reason }

func (e *ConfigError) Unwrap() error { return ErrConfigInvalid }
