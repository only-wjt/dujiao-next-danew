package contract

import (
	"context"

	"github.com/dujiao-next/internal/config"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

// SettingReader 提供 Telegram 动态配置。
type SettingReader interface {
	GetTelegramAuthSetting(defaultCfg config.TelegramAuthConfig) (settingssecurity.TelegramAuthSetting, error)
	GetNotificationCenterSetting() (settingsmessaging.NotificationCenterSetting, error)
}

// Sender 是应用层发送显式 Bot Token 消息所需的端口。
type Sender interface {
	SendWithBotToken(ctx context.Context, botToken string, options SendOptions) error
}
