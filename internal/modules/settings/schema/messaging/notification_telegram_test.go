package settingsmessaging

import (
	"reflect"
	"testing"

	settingsvalue "github.com/dujiao-next/internal/modules/settings/schema/value"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestNotificationCenterTelegramPatchKeepsBotToken(t *testing.T) {
	current := NotificationCenterDefaultSetting()
	current.Channels.Telegram.BotToken = "saved-token"
	enabled := true
	emptyToken := "  "
	recipients := []string{" 10001 ", "10001", "-10002"}

	next, err := ApplyNotificationCenterSettingPatch(current, NotificationCenterSettingPatch{
		Channels: &NotificationChannelsPatch{
			Telegram: &NotificationChannelPatch{
				Enabled:    &enabled,
				BotToken:   &emptyToken,
				Recipients: &recipients,
			},
		},
	})
	if err != nil {
		t.Fatalf("apply telegram patch: %v", err)
	}
	if next.Channels.Telegram.BotToken != "saved-token" {
		t.Fatalf("empty patch must preserve bot token, got %q", next.Channels.Telegram.BotToken)
	}
	if !reflect.DeepEqual(next.Channels.Telegram.Recipients, []string{"10001", "-10002"}) {
		t.Fatalf("unexpected recipients: %#v", next.Channels.Telegram.Recipients)
	}
}

func TestNotificationCenterTelegramMaskAndPersistenceRoundTrip(t *testing.T) {
	setting := NotificationCenterDefaultSetting()
	setting.Channels.Telegram = NotificationChannelSetting{
		Enabled:    true,
		BotToken:   "top-secret-token",
		Recipients: []string{"10001"},
	}

	encoded := jsonmap.JSON(NotificationCenterSettingToMap(setting))
	decoded := DecodeNotificationCenterSetting(encoded, NotificationCenterDefaultSetting())
	if decoded.Channels.Telegram.BotToken != "top-secret-token" {
		t.Fatalf("persisted bot token was not decoded: %q", decoded.Channels.Telegram.BotToken)
	}
	if !reflect.DeepEqual(decoded.Channels.Telegram.Recipients, []string{"10001"}) {
		t.Fatalf("unexpected decoded recipients: %#v", decoded.Channels.Telegram.Recipients)
	}

	masked := MaskNotificationCenterSettingForAdmin(setting)
	channels := settingsvalue.ToStringAnyMap(masked["channels"])
	telegram := settingsvalue.ToStringAnyMap(channels["telegram"])
	if telegram["bot_token"] != "" {
		t.Fatalf("admin response leaked bot token: %#v", telegram["bot_token"])
	}
	if telegram["has_bot_token"] != true {
		t.Fatalf("admin response must expose configured state: %#v", telegram["has_bot_token"])
	}
}
