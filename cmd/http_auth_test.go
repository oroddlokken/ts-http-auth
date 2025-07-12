package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/oroddlokken/tailscale-http-auth/cmd/config"
	"github.com/patrickmn/go-cache"
	"tailscale.com/client/tailscale/apitype"
	tailscale "tailscale.com/client/tailscale/v2"
	"tailscale.com/tailcfg"
)

// --- Mock clients ---

type mockLocalClient struct {
	whoisFn func(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

func (m *mockLocalClient) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	return m.whoisFn(ctx, remoteAddr)
}

type mockDevicesClient struct {
	getFn func(ctx context.Context, deviceID string) (*tailscale.Device, error)
}

func (m *mockDevicesClient) Get(ctx context.Context, deviceID string) (*tailscale.Device, error) {
	return m.getFn(ctx, deviceID)
}

type mockUsersClient struct {
	listFn func(ctx context.Context, userType *tailscale.UserType, role *tailscale.UserRole) ([]tailscale.User, error)
}

func (m *mockUsersClient) List(ctx context.Context, userType *tailscale.UserType, role *tailscale.UserRole) ([]tailscale.User, error) {
	return m.listFn(ctx, userType, role)
}

// --- Test helpers ---

func testConfig() config.Config {
	return config.Config{
		Cache: config.CacheConfig{
			DeviceTTL: 1800,
			UserTTL:   900,
			UsersTTL:  15,
		},
		Logging: config.LoggingConfig{
			Level:              "error",
			AddSource:          false,
			RequestHeaderLevel: "debug",
		},
		Response: config.ResponseConfig{
			DeviceLookup:                   false,
			UserLookup:                     false,
			SetClientDeviceAddressesHeader: true,
			SetClientOsHeader:              true,
			SetClientTagsHeader:            true,
			SetClientVersionHeader:         true,
			SetClientDeviceNameHeader:      true,
			SetUserProfilePicUrlHeader:     true,
			SetUserCreatedHeader:           true,
			SetUserTypeHeader:              true,
			SetUserRoleHeader:              true,
		},
		HttpApi: config.HttpApiConfig{
			Mode: "http",
			Host: "",
			Port: 12999,
		},
	}
}

func newTestService() *Service {
	cfg := testConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	c := cache.New(15*time.Second, 20*time.Second)
	service := &Service{
		cache:              c,
		config:             cfg,
		logger:             logger,
		requestHeaderLevel: slog.LevelDebug,
	}
	service.httpApi = NewHttpApi(service)
	return service
}

func newTestServiceWithMocks(cfg config.Config, lc localClient, dc devicesClient, uc usersClient) *Service {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	c := cache.New(15*time.Second, 20*time.Second)
	service := &Service{
		cache:              c,
		localClient:        lc,
		devices:            dc,
		users:              uc,
		config:             cfg,
		logger:             logger,
		requestHeaderLevel: slog.LevelDebug,
	}
	service.httpApi = NewHttpApi(service)
	return service
}

func testWhoisResponse() *apitype.WhoIsResponse {
	return &apitype.WhoIsResponse{
		Node: &tailcfg.Node{
			ID:       12345,
			StableID: "nStable123",
			Name:     "mydevice.my-tailnet.ts.net.",
			Addresses: []netip.Prefix{
				netip.MustParsePrefix("100.64.0.1/32"),
			},
			Hostinfo: (&tailcfg.Hostinfo{OS: "iOS"}).View(),
		},
		UserProfile: &tailcfg.UserProfile{
			LoginName:   "alice@example.com",
			DisplayName: "Alice Smith",
		},
	}
}

func testDevice() *tailscale.Device {
	return &tailscale.Device{
		ID:            "12345",
		Authorized:    true,
		IsExternal:    false,
		User:          "alice@example.com",
		Tags:          []string{"tag:trusted"},
		ClientVersion: "1.80.2",
	}
}

func testUsers() []tailscale.User {
	return []tailscale.User{
		{
			ID:            "user-1",
			TailnetID:     "tailnet-1",
			DisplayName:   "Alice Smith",
			LoginName:     "alice@example.com",
			ProfilePicURL: "https://example.com/alice.jpg",
			Created:       time.Date(2024, 5, 11, 18, 35, 0, 0, time.UTC),
			Type:          "member",
			Role:          "owner",
		},
	}
}

func authRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Remote-Addr", "100.64.0.1")
	req.Header.Set("Remote-Port", "12345")
	return req
}

// --- Tests ---

func TestHealthz(t *testing.T) {
	service := newTestService()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	service.httpApi.healthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if body != `{"status": "ok"}` {
		t.Errorf("healthz body = %q, want %q", body, `{"status": "ok"}`)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("healthz Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestHandler_MissingRemoteAddr(t *testing.T) {
	service := newTestService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Remote-Port", "12345")
	rec := httptest.NewRecorder()

	service.httpApi.handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandler_MissingRemotePort(t *testing.T) {
	service := newTestService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Remote-Addr", "100.64.0.1")
	rec := httptest.NewRecorder()

	service.httpApi.handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandler_InvalidAddrPort(t *testing.T) {
	service := newTestService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Remote-Addr", "not-an-ip")
	req.Header.Set("Remote-Port", "12345")
	rec := httptest.NewRecorder()

	service.httpApi.handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MissingBothHeaders(t *testing.T) {
	service := newTestService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	service.httpApi.handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandler_WhoisOnly(t *testing.T) {
	cfg := testConfig()
	lc := &mockLocalClient{whoisFn: func(_ context.Context, addr string) (*apitype.WhoIsResponse, error) {
		if addr != "100.64.0.1:12345" {
			t.Errorf("WhoIs called with %q, want %q", addr, "100.64.0.1:12345")
		}
		return testWhoisResponse(), nil
	}}

	service := newTestServiceWithMocks(cfg, lc, nil, nil)
	rec := httptest.NewRecorder()
	service.httpApi.handler(rec, authRequest())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	h := rec.Header()
	checks := map[string]string{
		"X-Tailscale-Device-Id":        "12345",
		"X-Tailscale-Device-Node-Id":   "nStable123",
		"X-Tailscale-Device-Os":        "iOS",
		"X-Tailscale-Device-Name":      "mydevice.my-tailnet.ts.net.",
		"X-Tailscale-Device-Addresses": "100.64.0.1",
	}
	for header, want := range checks {
		if got := h.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestHandler_WithDeviceLookup(t *testing.T) {
	cfg := testConfig()
	cfg.Response.DeviceLookup = true

	lc := &mockLocalClient{whoisFn: func(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
		return testWhoisResponse(), nil
	}}
	dc := &mockDevicesClient{getFn: func(_ context.Context, id string) (*tailscale.Device, error) {
		return testDevice(), nil
	}}

	service := newTestServiceWithMocks(cfg, lc, dc, nil)
	rec := httptest.NewRecorder()
	service.httpApi.handler(rec, authRequest())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	h := rec.Header()
	checks := map[string]string{
		"X-Tailscale-Device-Authorized":    "true",
		"X-Tailscale-Device-External":      "false",
		"X-Tailscale-Device-Tags":          "tag:trusted",
		"X-Tailscale-Device-Client-Version": "1.80.2",
	}
	for header, want := range checks {
		if got := h.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestHandler_WithDeviceAndUserLookup(t *testing.T) {
	cfg := testConfig()
	cfg.Response.DeviceLookup = true
	cfg.Response.UserLookup = true

	lc := &mockLocalClient{whoisFn: func(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
		return testWhoisResponse(), nil
	}}
	dc := &mockDevicesClient{getFn: func(_ context.Context, _ string) (*tailscale.Device, error) {
		return testDevice(), nil
	}}
	uc := &mockUsersClient{listFn: func(_ context.Context, _ *tailscale.UserType, _ *tailscale.UserRole) ([]tailscale.User, error) {
		return testUsers(), nil
	}}

	service := newTestServiceWithMocks(cfg, lc, dc, uc)
	rec := httptest.NewRecorder()
	service.httpApi.handler(rec, authRequest())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	h := rec.Header()
	checks := map[string]string{
		"X-Tailscale-User-Id":              "user-1",
		"X-Tailscale-User-Tailnet-Id":      "tailnet-1",
		"X-Tailscale-User-Display-Name":    "Alice Smith",
		"X-Tailscale-Device-User":          "alice@example.com",
		"X-Tailscale-User-Profile-Pic-Url": "https://example.com/alice.jpg",
		"X-Tailscale-User-Created":         "2024-05-11T18:35:00Z",
		"X-Tailscale-User-Type":            "member",
		"X-Tailscale-User-Role":            "owner",
	}
	for header, want := range checks {
		if got := h.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestHandler_TailnetMismatch(t *testing.T) {
	cfg := testConfig()
	cfg.Tailscale.ExpectedTailnet = "other-tailnet.ts.net"

	lc := &mockLocalClient{whoisFn: func(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
		return testWhoisResponse(), nil
	}}

	service := newTestServiceWithMocks(cfg, lc, nil, nil)
	rec := httptest.NewRecorder()
	service.httpApi.handler(rec, authRequest())

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandler_TailnetMatch(t *testing.T) {
	cfg := testConfig()
	cfg.Tailscale.ExpectedTailnet = "my-tailnet.ts.net"

	lc := &mockLocalClient{whoisFn: func(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
		return testWhoisResponse(), nil
	}}

	service := newTestServiceWithMocks(cfg, lc, nil, nil)
	rec := httptest.NewRecorder()
	service.httpApi.handler(rec, authRequest())

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestHandler_WhoisError(t *testing.T) {
	cfg := testConfig()
	lc := &mockLocalClient{whoisFn: func(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
		return nil, fmt.Errorf("whois failed")
	}}

	service := newTestServiceWithMocks(cfg, lc, nil, nil)
	rec := httptest.NewRecorder()
	service.httpApi.handler(rec, authRequest())

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- Cache tests ---

func TestGetDeviceInfo_CacheHit(t *testing.T) {
	cfg := testConfig()
	callCount := 0
	dc := &mockDevicesClient{getFn: func(_ context.Context, _ string) (*tailscale.Device, error) {
		callCount++
		return testDevice(), nil
	}}

	service := newTestServiceWithMocks(cfg, nil, dc, nil)
	ctx := context.Background()

	// First call: cache miss
	d1, err := service.getDeviceInfo(ctx, "12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d1.ID != "12345" {
		t.Errorf("device ID = %q, want %q", d1.ID, "12345")
	}
	if callCount != 1 {
		t.Errorf("API called %d times, want 1", callCount)
	}

	// Second call: cache hit, no additional API call
	d2, err := service.getDeviceInfo(ctx, "12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d2.ID != "12345" {
		t.Errorf("device ID = %q, want %q", d2.ID, "12345")
	}
	if callCount != 1 {
		t.Errorf("API called %d times after cache hit, want 1", callCount)
	}
}

func TestGetUsers_CacheHit(t *testing.T) {
	cfg := testConfig()
	callCount := 0
	uc := &mockUsersClient{listFn: func(_ context.Context, _ *tailscale.UserType, _ *tailscale.UserRole) ([]tailscale.User, error) {
		callCount++
		return testUsers(), nil
	}}

	service := newTestServiceWithMocks(cfg, nil, nil, uc)
	ctx := context.Background()

	// First call: cache miss
	users1, err := service.getUsers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users1) != 1 {
		t.Errorf("got %d users, want 1", len(users1))
	}
	if callCount != 1 {
		t.Errorf("API called %d times, want 1", callCount)
	}

	// Second call: cache hit
	users2, err := service.getUsers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users2) != 1 {
		t.Errorf("got %d users, want 1", len(users2))
	}
	if callCount != 1 {
		t.Errorf("API called %d times after cache hit, want 1", callCount)
	}
}

func TestGetUserInfoByEmail_CacheHit(t *testing.T) {
	cfg := testConfig()
	callCount := 0
	uc := &mockUsersClient{listFn: func(_ context.Context, _ *tailscale.UserType, _ *tailscale.UserRole) ([]tailscale.User, error) {
		callCount++
		return testUsers(), nil
	}}

	service := newTestServiceWithMocks(cfg, nil, nil, uc)
	ctx := context.Background()

	// First call: cache miss (triggers getUsers)
	user1, err := service.getUserInfoByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user1.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName = %q, want %q", user1.DisplayName, "Alice Smith")
	}
	if callCount != 1 {
		t.Errorf("API called %d times, want 1", callCount)
	}

	// Second call: user-level cache hit (no additional API call)
	user2, err := service.getUserInfoByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user2.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName = %q, want %q", user2.DisplayName, "Alice Smith")
	}
	if callCount != 1 {
		t.Errorf("API called %d times after cache hit, want 1", callCount)
	}
}

func TestGetUserInfoByEmail_NotFound(t *testing.T) {
	cfg := testConfig()
	uc := &mockUsersClient{listFn: func(_ context.Context, _ *tailscale.UserType, _ *tailscale.UserRole) ([]tailscale.User, error) {
		return testUsers(), nil
	}}

	service := newTestServiceWithMocks(cfg, nil, nil, uc)
	ctx := context.Background()

	_, err := service.getUserInfoByEmail(ctx, "unknown@example.com")
	if err == nil {
		t.Error("expected error for unknown email, got nil")
	}
}

// --- Logger tests ---

func TestSetupLogger(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := setupLogger(tt.level, false)
			if logger == nil {
				t.Error("setupLogger returned nil")
			}
		})
	}
}

func TestSetupLogger_InvalidLevel(t *testing.T) {
	// setupLogger calls log.Fatalf on invalid level, which calls os.Exit(1).
	// We verify this by running the test in a subprocess.
	if os.Getenv("TEST_SETUP_LOGGER_INVALID") == "1" {
		setupLogger("invalid", false)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSetupLogger_InvalidLevel")
	cmd.Env = append(os.Environ(), "TEST_SETUP_LOGGER_INVALID=1")
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		return // expected: process exited with non-zero status
	}
	t.Error("setupLogger should exit on invalid level")
}

func TestSetupLogger_WithSource(t *testing.T) {
	logger := setupLogger("info", true)
	if logger == nil {
		t.Error("setupLogger returned nil")
	}
}
