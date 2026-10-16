// Package cdkissuebootstrap 装配 ZovoCard 实时发码模块。
package cdkissuebootstrap

import (
	"strconv"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	cdkissueapp "github.com/dujiao-next/internal/modules/cdkissue/application"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	"github.com/dujiao-next/internal/modules/cdkissue/infrastructure/zovocard"
	"github.com/dujiao-next/internal/queue"
)

// ProductStore 是读取商品所需的最小端口。
type ProductStore interface {
	GetByID(id string) (*productdomain.Product, error)
}

// SKUStore 是读取 SKU 所需的最小端口。
type SKUStore interface {
	ListByProduct(productID uint, onlyActive bool) ([]productdomain.ProductSKU, error)
}

// Dependencies 是模块装配依赖。
type Dependencies struct {
	Store        cdkissuecontract.Store
	Products     ProductStore
	SKUs         SKUStore
	Queue        *queue.Client
	AppSecretKey string
}

// New 创建实时发码服务。告警通知器在通知中心构造后通过 SetAlertNotifier 注入。
func New(deps Dependencies) *cdkissueapp.Service {
	return cdkissueapp.NewService(cdkissueapp.Options{
		Store:        deps.Store,
		Gateway:      zovocard.New(nil),
		Products:     productReader{products: deps.Products, skus: deps.SKUs},
		Fulfillments: fulfillmentEnqueuer{client: deps.Queue},
		AppSecretKey: deps.AppSecretKey,
	})
}

type productReader struct {
	products ProductStore
	skus     SKUStore
}

func (r productReader) GetProduct(productID uint) (*cdkissuecontract.Product, error) {
	product, err := r.products.GetByID(strconv.FormatUint(uint64(productID), 10))
	if err != nil || product == nil {
		return nil, err
	}
	skus, err := r.skus.ListByProduct(productID, false)
	if err != nil {
		return nil, err
	}
	out := &cdkissuecontract.Product{ID: product.ID, FulfillmentType: product.FulfillmentType}
	for _, sku := range skus {
		out.SKUs = append(out.SKUs, cdkissuecontract.ProductSKU{ID: sku.ID, Code: sku.SKUCode, IsActive: sku.IsActive})
	}
	return out, nil
}

type fulfillmentEnqueuer struct {
	client *queue.Client
}

func (e fulfillmentEnqueuer) Enabled() bool {
	return e.client != nil && e.client.Enabled()
}

func (e fulfillmentEnqueuer) EnqueueAutoFulfill(orderID uint) error {
	return e.client.EnqueueOrderAutoFulfill(queue.OrderAutoFulfillPayload{OrderID: orderID})
}
