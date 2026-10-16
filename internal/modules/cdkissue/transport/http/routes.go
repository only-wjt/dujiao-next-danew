package cdkissuehttp

import "github.com/gin-gonic/gin"

// RegisterAdminRoutes 注册后台 ZovoCard 实时发码路由。
func RegisterAdminRoutes(admin gin.IRoutes, handler *AdminHandler) {
	if admin == nil || handler == nil {
		panic("cdkissue admin routes: required dependency is nil")
	}
	admin.GET("/cdk-issue/config", handler.GetConfig)
	admin.PUT("/cdk-issue/config", handler.UpdateConfig)
	admin.POST("/cdk-issue/config/test", handler.TestConnection)
	admin.GET("/cdk-issue/overview", handler.GetOverview)
	admin.GET("/cdk-issue/products/:id/binding", handler.GetBinding)
	admin.PUT("/cdk-issue/products/:id/binding", handler.SaveBinding)
	admin.GET("/cdk-issue/requests", handler.ListRequests)
	admin.GET("/cdk-issue/requests/stats", handler.GetStats)
	admin.POST("/cdk-issue/requests/:id/retry", handler.RetryRequest)
	admin.GET("/cdk-issue/orders/:id/requests", handler.ListOrderRequests)
	admin.POST("/cdk-issue/orders/:id/disable-codes", handler.DisableOrderCodes)
}
