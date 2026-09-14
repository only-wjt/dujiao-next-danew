package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dujiao-next/internal/config"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
	notifycontract "github.com/dujiao-next/internal/modules/telegram/notify/contract"
)

type settingReaderStub struct {
	notifySetting settingsmessaging.NotificationCenterSetting
	notifyErr     error
	setting       settingssecurity.TelegramAuthSetting
	err           error
	seen          config.TelegramAuthConfig
}

func (s *settingReaderStub) GetNotificationCenterSetting() (settingsmessaging.NotificationCenterSetting, error) {
	return s.notifySetting, s.notifyErr
}

func (s *settingReaderStub) GetTelegramAuthSetting(defaultCfg config.TelegramAuthConfig) (settingssecurity.TelegramAuthSetting, error) {
	s.seen = defaultCfg
	return s.setting, s.err
}

type senderStub struct {
	token   string
	options notifycontract.SendOptions
	err     error
	calls   int
}

func (s *senderStub) SendWithBotToken(_ context.Context, token string, options notifycontract.SendOptions) error {
	s.calls++
	s.token = token
	s.options = options
	return s.err
}

func TestServiceSendMessagePrefersNotificationCenterBotToken(t *testing.T) {
	notifySetting := settingsmessaging.NotificationCenterDefaultSetting()
	notifySetting.Channels.Telegram.BotToken = "  notify-token  "
	settings := &settingReaderStub{
		notifySetting: notifySetting,
		setting:       settingssecurity.TelegramAuthSetting{BotToken: "login-token"},
	}
	sender := &senderStub{}
	service := NewService(settings, config.TelegramAuthConfig{BotToken: "default-token"}, sender)

	if err := service.SendMessage(context.Background(), "10001", "hello"); err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if sender.calls != 1 || sender.token != "notify-token" {
		t.Fatalf("unexpected sender call: calls=%d token=%q", sender.calls, sender.token)
	}
}

func TestServiceSendMessageUsesDynamicBotToken(t *testing.T) {
	defaults := config.TelegramAuthConfig{BotToken: "default-token"}
	settings := &settingReaderStub{
		setting: settingssecurity.TelegramAuthSetting{BotToken: "  dynamic-token  "},
	}
	sender := &senderStub{}
	service := NewService(settings, defaults, sender)

	if err := service.SendMessage(context.Background(), "10001", "hello"); err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if settings.seen != defaults {
		t.Fatalf("unexpected default config: %#v", settings.seen)
	}
	if sender.calls != 1 || sender.token != "dynamic-token" {
		t.Fatalf("unexpected sender call: calls=%d token=%q", sender.calls, sender.token)
	}
	if sender.options.ChatID != "10001" || sender.options.Message != "hello" {
		t.Fatalf("unexpected send options: %#v", sender.options)
	}
	if sender.options.ParseMode != "HTML" {
		t.Fatalf("telegram notifications must use HTML parse mode, got %q", sender.options.ParseMode)
	}
	if !sender.options.DisableWebPagePreview {
		t.Fatal("configured notification must disable web page previews")
	}
}

func TestServiceSendMessageFallsBackToDefaultWithoutSettings(t *testing.T) {
	sender := &senderStub{}
	service := NewService(nil, config.TelegramAuthConfig{BotToken: "  default-token  "}, sender)

	if err := service.SendMessage(context.Background(), "10001", "hello"); err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if sender.calls != 1 || sender.token != "default-token" {
		t.Fatalf("unexpected sender call: calls=%d token=%q", sender.calls, sender.token)
	}
}

func TestServiceSendMessageWithBotTokenUsesExplicitToken(t *testing.T) {
	sender := &senderStub{}
	service := NewService(&settingReaderStub{}, config.TelegramAuthConfig{}, sender)

	if err := service.SendMessageWithBotToken(context.Background(), "  form-token  ", "10001", "hello"); err != nil {
		t.Fatalf("send with form token failed: %v", err)
	}
	if sender.calls != 1 || sender.token != "form-token" {
		t.Fatalf("unexpected sender call: calls=%d token=%q", sender.calls, sender.token)
	}
	if sender.options.ChatID != "10001" || sender.options.Message != "hello" {
		t.Fatalf("unexpected send options: %#v", sender.options)
	}
}

func TestServiceSendMessageRejectsMissingBotToken(t *testing.T) {
	sender := &senderStub{}
	service := NewService(&settingReaderStub{}, config.TelegramAuthConfig{}, sender)

	err := service.SendMessage(context.Background(), "10001", "hello")
	if !errors.Is(err, notifycontract.ErrNotifyConfigInvalid) {
		t.Fatalf("expected config invalid error, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender must not be called without a bot token, got %d calls", sender.calls)
	}
}

func TestServiceSendMessagePropagatesSettingReadFailure(t *testing.T) {
	wantErr := errors.New("settings unavailable")
	sender := &senderStub{}
	service := NewService(&settingReaderStub{err: wantErr}, config.TelegramAuthConfig{BotToken: "default-token"}, sender)

	err := service.SendMessage(context.Background(), "10001", "hello")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected setting error, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender must not be called after a setting error, got %d calls", sender.calls)
	}
}
