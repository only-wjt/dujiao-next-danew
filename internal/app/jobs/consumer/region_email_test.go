package consumer

import (
	"testing"

	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func realtimeItem(country string) orderdomain.OrderItem {
	snapshot := jsonmap.JSON{}
	cdkissuedomain.ApplyItemSnapshot(snapshot, "plus", country)
	return orderdomain.OrderItem{SKUSnapshotJSON: snapshot}
}

func TestFulfillmentEmailPayloadAddsRegionLine(t *testing.T) {
	order := &orderdomain.Order{
		Items:       []orderdomain.OrderItem{realtimeItem("PH")},
		Fulfillment: &fulfillmentdomain.Fulfillment{Payload: "ZC-1\nZC-2"},
	}
	if got := buildOrderFulfillmentEmailPayloadForLocale(order, "zh-CN"); got != "付款地区：PH 菲律宾\nZC-1\nZC-2" {
		t.Fatalf("zh-CN payload = %q", got)
	}
	if got := buildOrderFulfillmentEmailPayloadForLocale(order, "en-US"); got != "Payment region: PH Philippines\nZC-1\nZC-2" {
		t.Fatalf("en-US payload = %q", got)
	}
	// 旧调用不加地区。
	if got := buildOrderFulfillmentEmailPayload(order); got != "ZC-1\nZC-2" {
		t.Fatalf("payload without locale = %q", got)
	}
}

func TestFulfillmentEmailPayloadRegionPerChildAndStock(t *testing.T) {
	order := &orderdomain.Order{
		Children: []orderdomain.Order{
			{OrderNo: "C1", Items: []orderdomain.OrderItem{realtimeItem("US")}, Fulfillment: &fulfillmentdomain.Fulfillment{Payload: "ZC-US"}},
			{OrderNo: "C2", Items: []orderdomain.OrderItem{{SKUSnapshotJSON: jsonmap.JSON{}}}, Fulfillment: &fulfillmentdomain.Fulfillment{Payload: "STOCK"}},
		},
	}
	want := "[C1]\n付款地区：US 美国\nZC-US\n\n[C2]\nSTOCK"
	if got := buildOrderFulfillmentEmailPayloadForLocale(order, "zh-CN"); got != want {
		t.Fatalf("payload = %q", got)
	}
}
