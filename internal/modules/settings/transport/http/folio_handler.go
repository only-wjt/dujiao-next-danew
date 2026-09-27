package settingshttp

import (
	"errors"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// FolioAdminService 是后台 Folio 对接设置端口。
type FolioAdminService interface {
	GetFolioSetting() (settingsintegration.FolioSetting, error)
	PatchFolioSetting(patch settingsintegration.FolioSettingPatch) (settingsintegration.FolioSetting, error)
	ApplyRuntime(setting settingsintegration.FolioSetting)
}

// FolioHandler 处理后台 Folio 对接设置请求。
type FolioHandler struct {
	folio FolioAdminService
}

func NewFolioHandler(folio FolioAdminService) *FolioHandler {
	if folio == nil {
		panic("settings folio handler: folio is nil")
	}
	return &FolioHandler{folio: folio}
}

// GetFolio 获取 Folio 对接配置（脱敏）。
func (h *FolioHandler) GetFolio(c *gin.Context) {
	setting, err := h.folio.GetFolioSetting()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, settingsintegration.MaskFolioSettingForAdmin(setting))
}

// UpdateFolio 更新 Folio 对接配置。
func (h *FolioHandler) UpdateFolio(c *gin.Context) {
	var req settingsintegration.FolioSettingPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	setting, err := h.folio.PatchFolioSetting(req)
	if err != nil {
		switch {
		case errors.Is(err, settingsintegration.ErrFolioConfigInvalid):
			ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, err.Error(), nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		}
		return
	}

	h.folio.ApplyRuntime(setting)
	response.Success(c, settingsintegration.MaskFolioSettingForAdmin(setting))
}
