package config

import (
	"context"
	"fmt"

	"github.com/sethvargo/go-envconfig"
)

type CacheConfig struct {
	DeviceTTL int `env:"CACHE_DEVICE_EXPIRY_SECONDS,default=1800"`
	UserTTL   int `env:"CACHE_GET_USER_EXPIRY_SECONDS,default=900"`
	UsersTTL  int `env:"CACHE_GET_USERS_EXPIRY_SECONDS,default=15"`
}

func (c *CacheConfig) Validate() error {
	if c.DeviceTTL <= 0 {
		return fmt.Errorf("invalid CACHE_DEVICE_EXPIRY_SECONDS: %d", c.DeviceTTL)
	}
	if c.UserTTL <= 0 {
		return fmt.Errorf("invalid CACHE_GET_USER_EXPIRY_SECONDS: %d", c.UserTTL)
	}
	if c.UsersTTL <= 0 {
		return fmt.Errorf("invalid CACHE_GET_USERS_EXPIRY_SECONDS: %d", c.UsersTTL)
	}
	return nil
}

type LoggingConfig struct {
	AddSource          bool   `env:"LOG_ADD_SOURCE,default=false"`
	Level              string `env:"LOG_LEVEL,default=info"`
	RequestHeaderLevel string `env:"LOG_REQUEST_HEADERS_LEVEL,default=debug"`
}

type TailscaleConfig struct {
	ClientID        string   `env:"TAILSCALE_OAUTH_CLIENT_ID,required"`
	ClientSecret    string   `env:"TAILSCALE_OAUTH_CLIENT_SECRET,required"`
	Tailnet         string   `env:"TAILSCALE_TAILNET,required"`
	ExpectedTailnet string   `env:"TAILSCALE_EXPECTED_TAILNET,default="`
	AllowedTags     []string `env:"TAILSCALE_ALLOWED_TAGS,default="`
}

type ResponseConfig struct {
	DeviceLookup bool `env:"TAILSCALE_DEVICE_LOOKUP,default=false"`
	UserLookup   bool `env:"TAILSCALE_USER_LOOKUP,default=false"`

	SetClientDeviceAddressesHeader bool `env:"SET_CLIENT_ADDRESSES_HEADER,default=true"`
	SetClientOsHeader              bool `env:"SET_CLIENT_OS_HEADER,default=true"`
	SetClientTagsHeader            bool `env:"SET_CLIENT_TAGS_HEADER,default=true"`
	SetClientVersionHeader         bool `env:"SET_CLIENT_VERSION_HEADER,default=true"`
	SetClientDeviceNameHeader      bool `env:"SET_DEVICE_NAME_HEADER,default=true"`

	SetUserProfilePicUrlHeader bool `env:"SET_USER_PROFILE_PIC_URL_HEADER,default=true"`
	SetUserCreatedHeader       bool `env:"SET_USER_CREATED_HEADER,default=true"`
	SetUserTypeHeader          bool `env:"SET_USER_TYPE_HEADER,default=true"`
	SetUserRoleHeader          bool `env:"SET_USER_ROLE_HEADER,default=true"`
}

type HttpApiConfig struct {
	Mode     string `env:"MODE,default=http"`
	SockPath string `env:"SOCK_PATH,default=/var/run/tailscale-http-auth.sock"`
	Host     string `env:"HOST,default="`
	Port     int    `env:"PORT,default=12999"`
}

type Config struct {
	Cache     CacheConfig
	Logging   LoggingConfig
	Tailscale TailscaleConfig
	Response  ResponseConfig
	HttpApi   HttpApiConfig
}

func NewConfig() (Config, error) {
	var cacheConfig CacheConfig
	if err := envconfig.Process(context.Background(), &cacheConfig); err != nil {
		return Config{}, fmt.Errorf("error loading cache configuration: %v", err)
	}
	if err := cacheConfig.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid cache configuration: %v", err)
	}

	var loggingConfig LoggingConfig
	if err := envconfig.Process(context.Background(), &loggingConfig); err != nil {
		return Config{}, fmt.Errorf("error loading logging configuration: %v", err)
	}

	var tailscaleConfig TailscaleConfig
	if err := envconfig.Process(context.Background(), &tailscaleConfig); err != nil {
		return Config{}, fmt.Errorf("error loading Tailscale configuration: %v", err)
	}

	var responseConfig ResponseConfig
	if err := envconfig.Process(context.Background(), &responseConfig); err != nil {
		return Config{}, fmt.Errorf("error loading response configuration: %v", err)
	}

	var httpApiConfig HttpApiConfig
	if err := envconfig.Process(context.Background(), &httpApiConfig); err != nil {
		return Config{}, fmt.Errorf("error loading HTTP API configuration: %v", err)
	}

	return Config{
		Cache:     cacheConfig,
		Logging:   loggingConfig,
		Tailscale: tailscaleConfig,
		Response:  responseConfig,
		HttpApi:   httpApiConfig,
	}, nil
}
