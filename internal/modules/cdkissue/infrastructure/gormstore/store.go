package gormstore

import (
	"errors"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store 是实时发码模块的 GORM 持久化实现。
type Store struct {
	db *gorm.DB
}

var _ cdkissuecontract.Store = (*Store)(nil)

// New 创建存储。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// GetConfig 读取配置；未保存时返回 nil。
func (s *Store) GetConfig(provider string) (*cdkissuedomain.Config, error) {
	var config cdkissuedomain.Config
	if err := s.db.Where("provider = ?", provider).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// SaveConfig 新建或更新配置。
func (s *Store) SaveConfig(config *cdkissuedomain.Config) error {
	return saveConfig(s.db, config)
}

// SaveConfigExclusive 保存启用中的渠道配置，并停用其它渠道。
func (s *Store) SaveConfigExclusive(config *cdkissuedomain.Config) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&cdkissuedomain.Config{}).
			Where("provider <> ? AND enabled = ?", config.Provider, true).
			Update("enabled", false).Error; err != nil {
			return err
		}
		return saveConfig(tx, config)
	})
}

func saveConfig(db *gorm.DB, config *cdkissuedomain.Config) error {
	if config.ID == 0 {
		return db.Create(config).Error
	}
	return db.Save(config).Error
}

// ListBindings 读取指定商品的全部绑定行。
func (s *Store) ListBindings(productIDs []uint) ([]cdkissuedomain.Binding, error) {
	rows := make([]cdkissuedomain.Binding, 0)
	if len(productIDs) == 0 {
		return rows, nil
	}
	err := s.db.Where("product_id IN ?", productIDs).Order("product_id ASC, sku_id ASC").Find(&rows).Error
	return rows, err
}

// ReplaceBindings 在事务内整体替换某商品的绑定。
func (s *Store) ReplaceBindings(productID uint, rows []cdkissuedomain.Binding) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("product_id = ?", productID).Delete(&cdkissuedomain.Binding{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

// ListEnabledProductIDs 返回启用实时发码的商品 ID；productIDs 为空时返回全部。
func (s *Store) ListEnabledProductIDs(productIDs []uint) ([]uint, error) {
	ids := make([]uint, 0)
	query := s.db.Model(&cdkissuedomain.Binding{}).Where("sku_id = 0 AND enabled = ?", true)
	if len(productIDs) > 0 {
		query = query.Where("product_id IN ?", productIDs)
	}
	err := query.Pluck("product_id", &ids).Error
	return ids, err
}

// CreateRequestIfAbsent 按订单项分片幂等建单。
func (s *Store) CreateRequestIfAbsent(request *cdkissuedomain.IssueRequest) (*cdkissuedomain.IssueRequest, error) {
	result := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(request)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		return request, nil
	}
	var existing cdkissuedomain.IssueRequest
	err := s.db.Where("order_id = ? AND order_item_id = ? AND chunk_index = ?", request.OrderID, request.OrderItemID, request.ChunkIndex).
		First(&existing).Error
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

// GetRequest 按 ID 读取。
func (s *Store) GetRequest(id uint) (*cdkissuedomain.IssueRequest, error) {
	var request cdkissuedomain.IssueRequest
	if err := s.db.First(&request, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &request, nil
}

// ListRequestsByOrder 读取单个（子）订单的请求。
func (s *Store) ListRequestsByOrder(orderID uint) ([]cdkissuedomain.IssueRequest, error) {
	rows := make([]cdkissuedomain.IssueRequest, 0)
	err := s.db.Where("order_id = ?", orderID).Order("order_item_id ASC, chunk_index ASC").Find(&rows).Error
	return rows, err
}

// ListRequestsByRootOrder 读取父订单及其子订单的全部请求。
func (s *Store) ListRequestsByRootOrder(rootOrderID uint) ([]cdkissuedomain.IssueRequest, error) {
	rows := make([]cdkissuedomain.IssueRequest, 0)
	err := s.db.Where("root_order_id = ?", rootOrderID).Order("order_id ASC, order_item_id ASC, chunk_index ASC").Find(&rows).Error
	return rows, err
}

func (s *Store) applyFilter(query *gorm.DB, filter cdkissuecontract.RequestFilter, withStatus bool) *gorm.DB {
	if withStatus {
		if status := strings.TrimSpace(filter.Status); status != "" {
			query = query.Where("status = ?", status)
		}
	}
	if plan := strings.TrimSpace(filter.Plan); plan != "" {
		query = query.Where("plan = ?", plan)
	}
	if orderNo := strings.TrimSpace(filter.OrderNo); orderNo != "" {
		query = query.Where("order_no LIKE ?", "%"+orderNo+"%")
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at <= ?", *filter.CreatedTo)
	}
	return query
}

// ListRequests 分页查询发码记录。
func (s *Store) ListRequests(filter cdkissuecontract.RequestFilter) ([]cdkissuedomain.IssueRequest, int64, error) {
	rows := make([]cdkissuedomain.IssueRequest, 0)
	query := s.applyFilter(s.db.Model(&cdkissuedomain.IssueRequest{}), filter, true)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := filter.Page, filter.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

// CountRequestsByStatus 统计各状态数量（不受 status 筛选影响）。
func (s *Store) CountRequestsByStatus(filter cdkissuecontract.RequestFilter) (map[string]int64, error) {
	type row struct {
		Status string
		Total  int64
	}
	var rows []row
	query := s.applyFilter(s.db.Model(&cdkissuedomain.IssueRequest{}), filter, false)
	if err := query.Select("status, COUNT(*) AS total").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, item := range rows {
		out[item.Status] = item.Total
	}
	return out, nil
}

// MarkOrderDelivered 交付成功后清空暂存密文。
func (s *Store) MarkOrderDelivered(orderID uint, deliveredAt time.Time) error {
	return s.db.Model(&cdkissuedomain.IssueRequest{}).
		Where("order_id = ? AND status = ?", orderID, cdkissuedomain.RequestStatusSucceeded).
		Updates(map[string]interface{}{
			"codes_encrypted": "",
			"delivered_at":    deliveredAt,
			"updated_at":      deliveredAt,
		}).Error
}

// ClaimRequest 原子地占用一个 pending 请求：同一时刻只有一个任务能调用卡台。
func (s *Store) ClaimRequest(id uint, now, until time.Time) (bool, error) {
	result := s.db.Model(&cdkissuedomain.IssueRequest{}).
		Where("id = ? AND status = ? AND (claimed_until IS NULL OR claimed_until < ?)", id, cdkissuedomain.RequestStatusPending, now).
		Updates(map[string]interface{}{
			"attempts":      gorm.Expr("attempts + 1"),
			"claimed_until": until,
			"updated_at":    now,
		})
	return result.RowsAffected == 1, result.Error
}

// UpdateRequestIfStatus 仅在状态未被其他任务改动时保存整行。
func (s *Store) UpdateRequestIfStatus(request *cdkissuedomain.IssueRequest, expectedStatus string) (bool, error) {
	result := s.db.Model(&cdkissuedomain.IssueRequest{}).
		Where("id = ? AND status = ?", request.ID, expectedStatus).
		Select("*").Omit("id", "created_at").
		Updates(request)
	return result.RowsAffected == 1, result.Error
}

// MarkCodesDisabled 只更新停用时间，避免覆盖并发写入的其他字段。
func (s *Store) MarkCodesDisabled(id uint, at time.Time) error {
	return s.db.Model(&cdkissuedomain.IssueRequest{}).Where("id = ?", id).
		Updates(map[string]interface{}{"codes_disabled_at": at, "updated_at": at}).Error
}

// ListAwaitingFulfillment 查询已付款、未交付的自动发货订单项（按订单表直接读取，供补发扫描使用）。
func (s *Store) ListAwaitingFulfillment(paidAfter, paidBefore time.Time, limit int) ([]cdkissuecontract.AwaitingOrderItem, error) {
	type row struct {
		OrderID     uint
		SKUSnapshot jsonmap.JSON `gorm:"column:sku_snapshot"`
	}
	rows := make([]row, 0)
	err := s.db.Table("order_items AS oi").
		Select("oi.order_id AS order_id, oi.sku_snapshot AS sku_snapshot").
		Joins("JOIN orders AS o ON o.id = oi.order_id AND o.deleted_at IS NULL").
		Joins("LEFT JOIN fulfillments AS f ON f.order_id = o.id AND f.deleted_at IS NULL").
		Where("o.status = ? AND o.paid_at > ? AND o.paid_at < ?", constants.OrderStatusPaid, paidAfter, paidBefore).
		Where("oi.deleted_at IS NULL AND oi.fulfillment_type = ? AND f.id IS NULL", constants.FulfillmentTypeAuto).
		Order("o.paid_at ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]cdkissuecontract.AwaitingOrderItem, 0, len(rows))
	for _, item := range rows {
		out = append(out, cdkissuecontract.AwaitingOrderItem{OrderID: item.OrderID, SKUSnapshot: item.SKUSnapshot})
	}
	return out, nil
}
