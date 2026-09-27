package application

import (
	"testing"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
)

func TestSignFolioRequestEmptyBody(t *testing.T) {
	// 与 Folio docs/integration-api.md §4.3 自测向量对齐
	const want = "0134d6cae7d2a6c1fcc1dc1d9ea85bcee3cab9c1b3b45a1647230ee3b079a0e8"
	got := SignFolioRequest("GET", "/api/v1/integrations/folio/orders/DJ1", "1700000000", "ABC", nil, "secret")
	if got != want {
		t.Fatalf("signature want %s got %s", want, got)
	}
	again := SignFolioRequest("GET", "/api/v1/integrations/folio/orders/DJ1", "1700000000", "ABC", []byte{}, "secret")
	if got != again {
		t.Fatalf("nil body and empty body differ")
	}
}

func TestResolveFolioRefundRootUsesParent(t *testing.T) {
	parentID := uint(10)
	child := &orderdomain.Order{ID: 11, OrderNo: "CHILD", ParentID: &parentID, InvoiceRequested: false}
	parent := &orderdomain.Order{ID: 10, OrderNo: "ROOT", InvoiceRequested: true}
	store := &fakeFolioLookup{byID: map[uint]*orderdomain.Order{10: parent, 11: child}}

	root, err := ResolveFolioRefundRoot(store, child)
	if err != nil {
		t.Fatal(err)
	}
	if root == nil || root.OrderNo != "ROOT" {
		t.Fatalf("want ROOT got %+v", root)
	}

	same, err := ResolveFolioRefundRoot(store, parent)
	if err != nil || same != parent {
		t.Fatalf("parent should pass through: err=%v got=%+v", err, same)
	}
}

type fakeFolioLookup struct {
	byID map[uint]*orderdomain.Order
}

func (f *fakeFolioLookup) GetByID(id uint) (*orderdomain.Order, error) {
	return f.byID[id], nil
}
