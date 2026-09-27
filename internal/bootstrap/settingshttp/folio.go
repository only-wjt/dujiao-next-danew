package settingsbootstrap

import (
	"github.com/dujiao-next/internal/config"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
)

type settingsFolioAdapter struct {
	settings *settingsapp.Service
	cfg      *config.Config
}

func (a settingsFolioAdapter) GetFolioSetting() (settingsintegration.FolioSetting, error) {
	return a.settings.GetFolioSetting(a.cfg.Folio)
}

func (a settingsFolioAdapter) PatchFolioSetting(patch settingsintegration.FolioSettingPatch) (settingsintegration.FolioSetting, error) {
	return a.settings.PatchFolioSetting(a.cfg.Folio, patch)
}

func (a settingsFolioAdapter) ApplyRuntime(setting settingsintegration.FolioSetting) {
	a.cfg.Folio = settingsintegration.FolioSettingToConfig(setting)
}
