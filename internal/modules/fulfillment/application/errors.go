package application

import (
	"errors"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
)

var (
	ErrFulfillmentInvalid      = productcontract.ErrFulfillmentInvalid
	ErrFulfillmentExists       = errors.New("fulfillment exists")
	ErrFulfillmentCreateFailed = errors.New("fulfillment create failed")
	ErrFulfillmentNotAuto      = errors.New("fulfillment not auto")
	ErrOrderNotFound           = orderapp.ErrOrderNotFound
	ErrOrderFetchFailed        = orderapp.ErrOrderFetchFailed
	ErrOrderStatusInvalid      = orderapp.ErrOrderStatusInvalid
	ErrOrderUpdateFailed       = orderapp.ErrOrderUpdateFailed
	ErrCardSecretInsufficient  = orderapp.ErrCardSecretInsufficient
	// ErrRealtimeIssueRetryable 实时发码结果待定，任务应稍后重试（沿用同一幂等键）。
	ErrRealtimeIssueRetryable = errors.New("realtime cdk issue retryable")
	// ErrRealtimeIssueHalted 实时发码需要管理员处理，任务不应继续重试。
	ErrRealtimeIssueHalted = errors.New("realtime cdk issue halted")
)
