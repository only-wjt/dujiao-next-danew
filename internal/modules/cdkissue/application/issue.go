package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dujiao-next/internal/crypto"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonslice"
)

const (
	errorCodeNotConfigured = "not_configured"
	errorCodeCountMismatch = "count_mismatch"
	errorCodeNetwork       = "network_error"
)

type itemChunkKey struct {
	itemID uint
	chunk  int
}

// RealtimeItems 返回订单中走实时发码的订单项：下单时已固化实时发码快照，或此前已建过发码请求。
// 只看下单快照而不看当前商品绑定，避免付款后修改绑定导致改用库存卡密或换套餐。
func (s *Service) RealtimeItems(order cdkissuecontract.IssueOrder) (map[uint]bool, error) {
	out := make(map[uint]bool)
	if len(order.Items) == 0 {
		return out, nil
	}
	existing, err := s.store.ListRequestsByOrder(order.OrderID)
	if err != nil {
		return nil, err
	}
	requested := make(map[uint]bool, len(existing))
	for _, request := range existing {
		requested[request.OrderItemID] = true
	}
	for _, item := range order.Items {
		if (item.Realtime && item.Plan != "") || requested[item.OrderItemID] {
			out[item.OrderItemID] = true
		}
	}
	return out, nil
}

// EnsureIssued 确保订单里的实时发码订单项都已拿到码，可安全重复调用。
//
// 返回 ErrIssueRetryable 表示应稍后由队列重试（同一幂等键）；
// 返回 ErrIssueHalted 表示需要管理员处理，队列不应继续重试。
func (s *Service) EnsureIssued(ctx context.Context, order cdkissuecontract.IssueOrder) (cdkissuecontract.IssuedItemCodes, error) {
	result := cdkissuecontract.IssuedItemCodes{}
	realtime, err := s.RealtimeItems(order)
	if err != nil {
		return nil, err
	}
	if len(realtime) == 0 {
		return result, nil
	}

	existingRows, err := s.store.ListRequestsByOrder(order.OrderID)
	if err != nil {
		return nil, err
	}
	existing := make(map[itemChunkKey]*cdkissuedomain.IssueRequest, len(existingRows))
	for i := range existingRows {
		row := &existingRows[i]
		existing[itemChunkKey{row.OrderItemID, row.ChunkIndex}] = row
	}

	config, err := s.loadConfig()
	if err != nil {
		return nil, err
	}

	rootOrderID := order.RootOrderID
	if rootOrderID == 0 {
		rootOrderID = order.OrderID
	}

	var credentials *cdkissuecontract.Credentials
	var credentialsErr error
	resolveCredentials := func() (*cdkissuecontract.Credentials, error) {
		if credentials == nil && credentialsErr == nil {
			value, err := s.credentials(config)
			if err != nil {
				credentialsErr = err
			} else {
				credentials = &value
			}
		}
		return credentials, credentialsErr
	}

	retryable, halted := false, false
	for _, item := range order.Items {
		if !realtime[item.OrderItemID] {
			continue
		}
		plan, country := item.Plan, item.PaymentCountry
		// 已建过请求时沿用请求上的套餐，保证同一订单项各分片口径一致。
		if first := existing[itemChunkKey{item.OrderItemID, 0}]; first != nil {
			plan, country = first.Plan, first.PaymentCountry
		}
		country = config.ResolveCountry(country)

		itemCodes := make([]string, 0, item.Quantity)
		itemComplete := true
		for chunkIndex, count := range cdkissuedomain.SplitCount(item.Quantity) {
			request := existing[itemChunkKey{item.OrderItemID, chunkIndex}]
			if request == nil {
				created, err := s.store.CreateRequestIfAbsent(&cdkissuedomain.IssueRequest{
					RootOrderID:    rootOrderID,
					OrderID:        order.OrderID,
					OrderNo:        order.OrderNo,
					OrderItemID:    item.OrderItemID,
					ChunkIndex:     chunkIndex,
					ProductID:      item.ProductID,
					ItemTitle:      item.Title,
					SKUID:          item.SKUID,
					Provider:       cdkissuedomain.ProviderZovoCard,
					Plan:           plan,
					PaymentCountry: country,
					Count:          count,
					IdempotencyKey: cdkissuedomain.BuildIdempotencyKey(order.OrderNo, item.OrderItemID, chunkIndex, 0),
					Status:         cdkissuedomain.RequestStatusPending,
					ProviderCDKIDs: jsonslice.Uints{},
				})
				if err != nil {
					return nil, err
				}
				request = created
				existing[itemChunkKey{item.OrderItemID, chunkIndex}] = created
			}

			codes, outcome := s.advanceRequest(ctx, request, resolveCredentials)
			switch outcome {
			case outcomeDone:
				itemCodes = append(itemCodes, codes...)
			case outcomeRetry:
				retryable = true
				itemComplete = false
			default:
				halted = true
				itemComplete = false
			}
		}
		if itemComplete {
			result[item.OrderItemID] = itemCodes
		}
	}

	switch {
	case retryable:
		return result, cdkissuecontract.ErrIssueRetryable
	case halted:
		return result, cdkissuecontract.ErrIssueHalted
	}
	return result, nil
}

type requestOutcome int

const (
	outcomeDone requestOutcome = iota
	outcomeRetry
	outcomeHalted
)

// MarkDelivered 交付事务成功后清除暂存的密文。
func (s *Service) MarkDelivered(orderID uint) error {
	return s.store.MarkOrderDelivered(orderID, s.now())
}

func (s *Service) encryptCodes(codes []string) (string, error) {
	payload, err := json.Marshal(codes)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(s.encryptKey, string(payload))
}

func (s *Service) decryptCodes(encrypted string) ([]string, error) {
	if strings.TrimSpace(encrypted) == "" {
		return nil, errors.New("codes already cleared")
	}
	plain, err := crypto.Decrypt(s.encryptKey, encrypted)
	if err != nil {
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal([]byte(plain), &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
