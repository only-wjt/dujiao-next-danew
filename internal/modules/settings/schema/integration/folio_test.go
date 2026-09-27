package settingsintegration

import (
	"reflect"
	"testing"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestApplyFolioSettingPatchClearsSecretWhenBaseURLEmpty(t *testing.T) {
	current := FolioSetting{BaseURL: "http://127.0.0.1:8090", Secret: "old-secret"}
	empty := ""
	updated, err := ApplyFolioSettingPatch(current, FolioSettingPatch{BaseURL: &empty})
	if err != nil {
		t.Fatalf("patch folio setting: %v", err)
	}
	if updated.BaseURL != "" || updated.Secret != "" {
		t.Fatalf("expected cleared setting, got %#v", updated)
	}
}

func TestApplyFolioSettingPatchKeepsSecretWhenEmpty(t *testing.T) {
	current := FolioSetting{BaseURL: "http://127.0.0.1:8090", Secret: "old-secret"}
	baseURL := "http://127.0.0.1:8091"
	emptySecret := ""
	updated, err := ApplyFolioSettingPatch(current, FolioSettingPatch{
		BaseURL: &baseURL,
		Secret:  &emptySecret,
	})
	if err != nil {
		t.Fatalf("patch folio setting: %v", err)
	}
	if updated.BaseURL != "http://127.0.0.1:8091" {
		t.Fatalf("base_url = %q, want updated", updated.BaseURL)
	}
	if updated.Secret != "old-secret" {
		t.Fatalf("secret = %q, want kept", updated.Secret)
	}
}

func TestValidateFolioSettingRequiresBothOrNeither(t *testing.T) {
	if err := ValidateFolioSetting(FolioSetting{}); err != nil {
		t.Fatalf("empty setting should be valid: %v", err)
	}
	if err := ValidateFolioSetting(FolioSetting{BaseURL: "http://127.0.0.1:8090"}); err == nil {
		t.Fatal("expected error when secret missing")
	}
	if err := ValidateFolioSetting(FolioSetting{Secret: "x"}); err == nil {
		t.Fatal("expected error when base_url missing")
	}
}

func TestMaskFolioSettingForAdmin(t *testing.T) {
	masked := MaskFolioSettingForAdmin(FolioSetting{
		BaseURL: "http://127.0.0.1:8090",
		Secret:  "secret",
	})
	want := jsonmap.JSON{
		"base_url":   "http://127.0.0.1:8090",
		"secret":     "",
		"has_secret": true,
		"enabled":    true,
	}
	if !reflect.DeepEqual(masked, want) {
		t.Fatalf("mask = %#v, want %#v", masked, want)
	}
	if !config.FolioEnabled(FolioSettingToConfig(FolioSetting{BaseURL: "http://x", Secret: "y"})) {
		t.Fatal("expected enabled when both set")
	}
}
