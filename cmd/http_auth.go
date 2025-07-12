package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/oroddlokken/tailscale-http-auth/cmd/config"
	"github.com/patrickmn/go-cache"
	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/client/tailscale/v2"
)

type localClient interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

type devicesClient interface {
	Get(ctx context.Context, deviceID string) (*tailscale.Device, error)
}

type usersClient interface {
	List(ctx context.Context, userType *tailscale.UserType, role *tailscale.UserRole) ([]tailscale.User, error)
}

const defaultExpiration = 15 * time.Second

var logLevelMap = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

func setupLogger(logLevel string, addSource bool) *slog.Logger {
	level, ok := logLevelMap[logLevel]
	if !ok {
		log.Fatalf("unknown log level: %s", logLevel)
	}

	logger := slog.New(slog.NewTextHandler(log.Writer(), &slog.HandlerOptions{
		Level:     level,
		AddSource: addSource,
	}))
	return logger
}

type Service struct {
	cache              *cache.Cache
	localClient        localClient
	devices            devicesClient
	users              usersClient
	config             config.Config
	logger             *slog.Logger
	requestHeaderLevel slog.Level
	httpApi            *HttpApi
}

func NewService(cfg config.Config, lc localClient, dc devicesClient, uc usersClient, c *cache.Cache, logger *slog.Logger) *Service {
	reqHeaderLevel, ok := logLevelMap[cfg.Logging.RequestHeaderLevel]
	if !ok {
		log.Fatalf("unknown request header log level: %s", cfg.Logging.RequestHeaderLevel)
	}

	service := &Service{
		cache:              c,
		localClient:        lc,
		devices:            dc,
		users:              uc,
		config:             cfg,
		logger:             logger,
		requestHeaderLevel: reqHeaderLevel,
	}
	service.httpApi = NewHttpApi(service)
	return service
}

func (s *Service) Start() {
	s.httpApi.Start()
}

// getDeviceInfo retrieves device information from the Tailscale API and caches it.
func (s *Service) getDeviceInfo(ctx context.Context, deviceId string) (*tailscale.Device, error) {
	deviceKey := "device:" + deviceId
	if cachedDevice, found := s.cache.Get(deviceKey); found {
		s.logger.Debug("Cache hit for device ID", "deviceId", deviceId)
		if device, ok := cachedDevice.(*tailscale.Device); ok {
			return device, nil
		}
		return nil, fmt.Errorf("cache entry for device %s is not of type *tailscale.Device", deviceId)
	}
	s.logger.Debug("Cache miss for device ID", "deviceId", deviceId)

	device, err := s.devices.Get(ctx, deviceId)
	if err != nil {
		return nil, fmt.Errorf("can't get device info ID %s: %w", deviceId, err)
	}
	s.logger.Debug("Storing device info in cache", "cacheKey", deviceKey)
	s.cache.Set(deviceKey, device, time.Duration(s.config.Cache.DeviceTTL)*time.Second)

	return device, nil
}

// getUsers retrieves all users from the Tailscale API and caches them.
func (s *Service) getUsers(ctx context.Context) ([]tailscale.User, error) {
	cacheKey := "users:all"
	if cached, found := s.cache.Get(cacheKey); found {
		s.logger.Debug("Cache hit for getUsers")
		if users, ok := cached.([]tailscale.User); ok {
			return users, nil
		}
		return nil, fmt.Errorf("cache entry for users is not of type []tailscale.User")
	}
	s.logger.Debug("Cache miss for getUsers")

	var userTypeMember tailscale.UserType = "all"
	var userRole tailscale.UserRole = "all"

	users, err := s.users.List(ctx, &userTypeMember, &userRole)
	if err != nil {
		return nil, fmt.Errorf("can't list users: %w", err)
	}
	s.logger.Debug("Storing users in cache", "cacheKey", cacheKey)
	s.cache.Set(cacheKey, users, time.Duration(s.config.Cache.UsersTTL)*time.Second)

	return users, nil
}

// getUserInfoByEmail retrieves user information by email from the Tailscale API and caches it.
func (s *Service) getUserInfoByEmail(ctx context.Context, email string) (*tailscale.User, error) {
	cacheKey := "user:email:" + email
	if cached, found := s.cache.Get(cacheKey); found {
		s.logger.Debug("Cache hit for getUserInfoByEmail", "email", email)
		if user, ok := cached.(*tailscale.User); ok {
			return user, nil
		}
		return nil, fmt.Errorf("cache entry for user %s is not of type *tailscale.User", email)
	}
	s.logger.Debug("Cache miss for getUserInfoByEmail", "email", email)

	users, err := s.getUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't get users: %w", err)
	}

	for _, user := range users {
		if user.LoginName == email {
			s.logger.Debug("Storing user info in cache", "cacheKey", cacheKey)
			s.cache.Set(cacheKey, &user, time.Duration(s.config.Cache.UserTTL)*time.Second)
			return &user, nil
		}
	}

	return nil, fmt.Errorf("user with email %s not found", email)
}

// HttpApi handles HTTP server logic and routing.
type HttpApi struct {
	service *Service
}

func NewHttpApi(service *Service) *HttpApi {
	return &HttpApi{
		service: service,
	}
}

func (api *HttpApi) handler(w http.ResponseWriter, r *http.Request) {
	api.service.logger.Log(r.Context(), api.service.requestHeaderLevel, "incoming request",
		"method", r.Method,
		"url", r.URL.String(),
		"remoteAddr", r.RemoteAddr,
		"headers", r.Header,
	)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	remoteAddr := r.Header.Get("Remote-Addr")
	if remoteAddr == "" {
		w.WriteHeader(http.StatusBadRequest)
		api.service.logger.Error("header Remote-Addr header is missing")
		return
	}

	remotePort := r.Header.Get("Remote-Port")
	if remotePort == "" {
		w.WriteHeader(http.StatusBadRequest)
		api.service.logger.Error("header Remote-Port header is missing")
		return
	}

	addr := fmt.Sprintf("%s:%s", remoteAddr, remotePort)
	if _, err := netip.ParseAddrPort(addr); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		api.service.logger.Error("can't look up remote address", "remoteAddr", remoteAddr, "port", remotePort, "error", err)
		return
	}

	service := api.service

	whois, err := api.service.localClient.WhoIs(ctx, addr)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		api.service.logger.Error("can't whois remote address", "remoteAddr", remoteAddr, "error", err)
		return
	}

	if expected := api.service.config.Tailscale.ExpectedTailnet; expected != "" {
		nodeName := strings.TrimSuffix(whois.Node.Name, ".")
		parts := strings.SplitN(nodeName, ".", 2)
		nodeTailnet := ""
		if len(parts) == 2 {
			nodeTailnet = parts[1]
		}
		if nodeTailnet != expected {
			w.WriteHeader(http.StatusForbidden)
			api.service.logger.Error("tailnet mismatch", "expected", expected, "got", nodeTailnet, "node", whois.Node.Name)
			return
		}
	}

	h := w.Header()
	deviceId := fmt.Sprintf("%d", whois.Node.ID)
	h.Set("X-Tailscale-Device-Id", deviceId)
	h.Set("X-Tailscale-Device-Node-Id", string(whois.Node.StableID))
	if api.service.config.Response.SetClientOsHeader {
		h.Set("X-Tailscale-Device-Os", whois.Node.Hostinfo.OS())
	}
	if api.service.config.Response.SetClientDeviceNameHeader {
		h.Set("X-Tailscale-Device-Name", whois.Node.Name)
	}

	if api.service.config.Response.SetClientDeviceAddressesHeader {
		addrs := make([]string, 0, len(whois.Node.Addresses))
		for _, addr := range whois.Node.Addresses {
			addrs = append(addrs, addr.Addr().String())
		}
		h.Set("X-Tailscale-Device-Addresses", strings.Join(addrs, ", "))
	}

	var device *tailscale.Device
	if api.service.config.Response.DeviceLookup {
		device, err = service.getDeviceInfo(ctx, deviceId)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			api.service.logger.Error("can't get device info", "deviceId", deviceId, "error", err)
			return
		}

		h.Set("X-Tailscale-Device-External", fmt.Sprintf("%t", device.IsExternal))
		h.Set("X-Tailscale-Device-Authorized", fmt.Sprintf("%t", device.Authorized))

		if api.service.config.Response.SetClientTagsHeader {
			h.Set("X-Tailscale-Device-Tags", strings.Join(device.Tags, ", "))
		}
		if api.service.config.Response.SetClientVersionHeader {
			h.Set("X-Tailscale-Device-Client-Version", device.ClientVersion)
		}
	}

	if api.service.config.Response.DeviceLookup && api.service.config.Response.UserLookup {
		user, err := service.getUserInfoByEmail(ctx, device.User)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			api.service.logger.Error("can't get user info by email", "email", device.User, "error", err)
			return
		}

		h.Set("X-Tailscale-User-Id", user.ID)
		h.Set("X-Tailscale-User-Tailnet-Id", user.TailnetID)
		h.Set("X-Tailscale-User-Display-Name", user.DisplayName)
		h.Set("X-Tailscale-Device-User", device.User)

		if api.service.config.Response.SetUserProfilePicUrlHeader {
			h.Set("X-Tailscale-User-Profile-Pic-Url", user.ProfilePicURL)
		}
		if api.service.config.Response.SetUserCreatedHeader {
			h.Set("X-Tailscale-User-Created", user.Created.Format(time.RFC3339))
		}
		if api.service.config.Response.SetUserTypeHeader {
			h.Set("X-Tailscale-User-Type", string(user.Type))
		}
		if api.service.config.Response.SetUserRoleHeader {
			h.Set("X-Tailscale-User-Role", string(user.Role))
		}
	}

	api.service.logger.Debug("successfully processed request")

	w.WriteHeader(http.StatusNoContent)
}

func (api *HttpApi) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

// startHttp starts the HTTP server on the configured host and port.
func (api *HttpApi) startHttp(mux *http.ServeMux) error {
	addr := fmt.Sprintf("%s:%d", api.service.config.HttpApi.Host, api.service.config.HttpApi.Port)
	api.service.logger.Info("listening on", "address", addr)

	if listenErr := http.ListenAndServe(addr, mux); listenErr != nil {
		wrappedErr := fmt.Errorf("failed to start HTTP server on %s: %w", addr, listenErr)
		api.service.logger.Error(wrappedErr.Error())
		return wrappedErr
	}

	return nil
}

func (api *HttpApi) startSock(mux *http.ServeMux) error {
	_ = os.Remove(api.service.config.HttpApi.SockPath) // might not exist, ignore error

	listeners, err := activation.Listeners()
	if err != nil {
		return fmt.Errorf("no sockets passed to this service with systemd: %w", err)
	}

	if len(listeners) > 0 {
		errCh := make(chan error, len(listeners))
		for _, ln := range listeners {
			go func(ln net.Listener) {
				api.service.logger.Info("listening with systemd socket", "addr", ln.Addr().String())
				if serveErr := http.Serve(ln, mux); serveErr != nil {
					errCh <- fmt.Errorf("socket server exited: %w", serveErr)
				}
			}(ln)
		}
		return <-errCh
	}

	ln, err := net.Listen("unix", api.service.config.HttpApi.SockPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", api.service.config.HttpApi.SockPath, err)
	}
	defer func() { _ = ln.Close() }()

	api.service.logger.Info("listening on socket", "socket", api.service.config.HttpApi.SockPath)
	if serveErr := http.Serve(ln, mux); serveErr != nil {
		return fmt.Errorf("socket server exited: %w", serveErr)
	}

	return nil
}

// Start starts the server based on the configured mode.
func (api *HttpApi) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", api.handler)
	mux.HandleFunc("/healthz", api.healthz)

	var err error
	switch api.service.config.HttpApi.Mode {
	case "http":
		err = api.startHttp(mux)
	case "sock":
		err = api.startSock(mux)
	}

	if err != nil {
		api.service.logger.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}

func main() {
	config, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if config.HttpApi.Mode != "http" && config.HttpApi.Mode != "sock" {
		log.Fatalf("Invalid mode: %s. Supported modes are 'http' or 'sock'", config.HttpApi.Mode)
	}

	logger := setupLogger(config.Logging.Level, config.Logging.AddSource)

	if config.Response.UserLookup && !config.Response.DeviceLookup {
		logger.Warn("Warning: User lookup is enabled, but device lookup is disabled. This will lead to incomplete user information.")
	}

	cache := cache.New(defaultExpiration, 20*time.Second)
	localClient := &local.Client{}
	v2Client := &tailscale.Client{
		Tailnet: config.Tailscale.Tailnet,
		HTTP: tailscale.OAuthConfig{
			ClientID:     config.Tailscale.ClientID,
			ClientSecret: config.Tailscale.ClientSecret,
			Scopes:       []string{"devices:core:read", "users:read"},
		}.HTTPClient(),
	}

	service := NewService(config, localClient, v2Client.Devices(), v2Client.Users(), cache, logger)
	service.Start()
}
