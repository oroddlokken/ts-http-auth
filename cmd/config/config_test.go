package config

import (
	"os"
	"testing"
)

func TestCacheConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  CacheConfig
		wantErr bool
	}{
		{
			name:    "valid defaults",
			config:  CacheConfig{DeviceTTL: 1800, UserTTL: 900, UsersTTL: 15},
			wantErr: false,
		},
		{
			name:    "zero DeviceTTL",
			config:  CacheConfig{DeviceTTL: 0, UserTTL: 900, UsersTTL: 15},
			wantErr: true,
		},
		{
			name:    "negative DeviceTTL",
			config:  CacheConfig{DeviceTTL: -1, UserTTL: 900, UsersTTL: 15},
			wantErr: true,
		},
		{
			name:    "zero UserTTL",
			config:  CacheConfig{DeviceTTL: 1800, UserTTL: 0, UsersTTL: 15},
			wantErr: true,
		},
		{
			name:    "zero UsersTTL",
			config:  CacheConfig{DeviceTTL: 1800, UserTTL: 900, UsersTTL: 0},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("CacheConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	t.Setenv("TAILSCALE_TAILNET", "test-tailnet")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_SECRET", "test-secret")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}

	// Check defaults
	if cfg.Cache.DeviceTTL != 1800 {
		t.Errorf("DeviceTTL = %d, want 1800", cfg.Cache.DeviceTTL)
	}
	if cfg.Cache.UserTTL != 900 {
		t.Errorf("UserTTL = %d, want 900", cfg.Cache.UserTTL)
	}
	if cfg.Cache.UsersTTL != 15 {
		t.Errorf("UsersTTL = %d, want 15", cfg.Cache.UsersTTL)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.Logging.Level, "info")
	}
	if cfg.Logging.AddSource != false {
		t.Errorf("AddSource = %v, want false", cfg.Logging.AddSource)
	}
	if cfg.HttpApi.Mode != "http" {
		t.Errorf("Mode = %q, want %q", cfg.HttpApi.Mode, "http")
	}
	if cfg.HttpApi.Port != 12999 {
		t.Errorf("Port = %d, want 12999", cfg.HttpApi.Port)
	}
	if cfg.Response.SetClientOsHeader != true {
		t.Errorf("SetClientOsHeader = %v, want true", cfg.Response.SetClientOsHeader)
	}
	if cfg.Response.DeviceLookup != false {
		t.Errorf("DeviceLookup = %v, want false", cfg.Response.DeviceLookup)
	}
	if len(cfg.Tailscale.AllowedTags) != 0 {
		t.Errorf("AllowedTags = %v, want empty", cfg.Tailscale.AllowedTags)
	}

	// Check required values
	if cfg.Tailscale.Tailnet != "test-tailnet" {
		t.Errorf("Tailnet = %q, want %q", cfg.Tailscale.Tailnet, "test-tailnet")
	}
	if cfg.Tailscale.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", cfg.Tailscale.ClientID, "test-client-id")
	}
}

func TestNewConfig_MissingRequired(t *testing.T) {
	// Unset required env vars to trigger validation error.
	// go-envconfig treats empty string as "set", so we must actually unset them.
	for _, key := range []string{
		"TAILSCALE_TAILNET",
		"TAILSCALE_OAUTH_CLIENT_ID",
		"TAILSCALE_OAUTH_CLIENT_SECRET",
	} {
		t.Setenv(key, "was-set")
		_ = os.Unsetenv(key)
	}

	_, err := NewConfig()
	if err == nil {
		t.Error("NewConfig() should fail when required env vars are missing")
	}
}

func TestNewConfig_InvalidCache(t *testing.T) {
	t.Setenv("TAILSCALE_TAILNET", "test")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_ID", "test")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_SECRET", "test")
	t.Setenv("CACHE_DEVICE_EXPIRY_SECONDS", "-1")

	_, err := NewConfig()
	if err == nil {
		t.Error("NewConfig() should fail with negative cache TTL")
	}
}

func TestNewConfig_CustomValues(t *testing.T) {
	t.Setenv("TAILSCALE_TAILNET", "my-tailnet")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_ID", "my-id")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_SECRET", "my-secret")
	t.Setenv("TAILSCALE_DEVICE_LOOKUP", "true")
	t.Setenv("TAILSCALE_USER_LOOKUP", "true")
	t.Setenv("MODE", "sock")
	t.Setenv("PORT", "8080")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SET_CLIENT_OS_HEADER", "false")
	t.Setenv("CACHE_DEVICE_EXPIRY_SECONDS", "60")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}

	if cfg.Response.DeviceLookup != true {
		t.Errorf("DeviceLookup = %v, want true", cfg.Response.DeviceLookup)
	}
	if cfg.Response.UserLookup != true {
		t.Errorf("UserLookup = %v, want true", cfg.Response.UserLookup)
	}
	if cfg.HttpApi.Mode != "sock" {
		t.Errorf("Mode = %q, want %q", cfg.HttpApi.Mode, "sock")
	}
	if cfg.HttpApi.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.HttpApi.Port)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Response.SetClientOsHeader != false {
		t.Errorf("SetClientOsHeader = %v, want false", cfg.Response.SetClientOsHeader)
	}
	if cfg.Cache.DeviceTTL != 60 {
		t.Errorf("DeviceTTL = %d, want 60", cfg.Cache.DeviceTTL)
	}
}

func TestNewConfig_AllowedTags(t *testing.T) {
	t.Setenv("TAILSCALE_TAILNET", "test-tailnet")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("TAILSCALE_OAUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("TAILSCALE_ALLOWED_TAGS", "tag:monitor,tag:admin")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	if len(cfg.Tailscale.AllowedTags) != 2 {
		t.Fatalf("AllowedTags = %v, want 2 entries", cfg.Tailscale.AllowedTags)
	}
	if cfg.Tailscale.AllowedTags[0] != "tag:monitor" {
		t.Errorf("AllowedTags[0] = %q, want %q", cfg.Tailscale.AllowedTags[0], "tag:monitor")
	}
	if cfg.Tailscale.AllowedTags[1] != "tag:admin" {
		t.Errorf("AllowedTags[1] = %q, want %q", cfg.Tailscale.AllowedTags[1], "tag:admin")
	}
}
