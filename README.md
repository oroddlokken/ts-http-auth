# Tailscale HTTP Auth for reverse proxies

Inspired by https://github.com/tailscale/tailscale/tree/main/cmd/nginx-auth, which does not support tagged devices.

This uses the Tailscale API to get more information about the client device. Your reverse proxy forwards the client's `Remote-Addr` and `Remote-Port` headers to this service, which does a whois lookup against the local Tailscale daemon and optionally enriches the response with device and user info from the Tailscale API.

# Prerequisites

- **Tailscale running on the host** - the service connects to the local Tailscale daemon via `/var/run/tailscale/tailscaled.sock`
- **Tailscale OAuth credentials** - create an OAuth client in the [Tailscale admin console](https://login.tailscale.com/admin/settings/oauth) with the scopes `devices:core:read` and `users:read`

# Quick start

Create a `.env` file:

```env
TAILSCALE_TAILNET=your-tailnet.ts.net
TAILSCALE_OAUTH_CLIENT_ID=your-client-id
TAILSCALE_OAUTH_CLIENT_SECRET=your-client-secret
```

Run with Docker Compose:

```yaml
# docker-compose.yml
services:
  tailscale-http-auth:
    image: ghcr.io/oroddlokken/ts-http-auth:latest
    container_name: tailscale-http-auth
    volumes:
      - /var/run/tailscale/tailscaled.sock:/var/run/tailscale/tailscaled.sock
    env_file:
      - .env
    networks:
      - web
    restart: unless-stopped
```

```sh
docker compose up -d
```

Or build and run directly:

```sh
go build -o http-auth ./cmd/http_auth.go
./http-auth
```

The service listens on port 12999 by default. Configure your reverse proxy to forward auth requests to it with `Remote-Addr` and `Remote-Port` headers set to the original client's address and port.

## Caddy

Use Caddy's [`forward_auth`](https://caddyserver.com/docs/caddyfile/directives/forward_auth) directive. The container should share a Docker network with Caddy.

In your Caddyfile, create a reusable snippet that gates access to Tailscale IPs and forwards auth to the service:

```caddyfile
(handle_auth) {
    @tailscale_ip remote_ip 100.0.0.0/8

    handle @tailscale_ip {
        forward_auth tailscale-http-auth:12999 {
            uri /
            header_up Remote-Addr {remote_host}
            header_up Remote-Port {remote_port}
            header_up Original-URI {uri}
            copy_headers {
                X-Tailscale-Device-Name
                X-Tailscale-Device-Id
                X-Tailscale-User-Display-Name
                X-Tailscale-User-Id
            }
        }

        reverse_proxy {args.0}
    }

    respond "Access denied" 403
}
```

Then use it per-site with `import handle_auth <upstream>`:

```caddyfile
myapp.example.com {
    import handle_auth myapp:8080
}
```

# Response headers

By default, only a whois is done against the local Tailscale daemon.
Assuming the client IP is valid, you will get a 204 response with these headers:

```
< HTTP/1.1 204 No Content
< X-Tailscale-Device-Name: ipad.janky-gorilla.ts.net.
< X-Tailscale-Device-Id: 1234567891123456
< X-Tailscale-Device-Node-Id: abCGE68Dz2FG                                             # Also known as Stable ID
< X-Tailscale-Device-Addresses: 100.16.200.50, fa4a:225c:31e0:ca12:4544:aa96:6247:800b # Disable with SET_CLIENT_ADDRESSES_HEADER=false
< X-Tailscale-Device-Os: iOS                                                           # Disable with SET_CLIENT_OS_HEADER=false
```

Additional device information can be retrieved by setting `TAILSCALE_DEVICE_LOOKUP=true`.
This does a request to the Tailscale Devices API.
```
< X-Tailscale-Device-Authorized: true
< X-Tailscale-Device-External: false
< X-Tailscale-Device-Client-Version: 1.80.2-t62b8bf6a0-g3c35ee987  # Disable with SET_CLIENT_VERSION_HEADER=false
< X-Tailscale-Device-Tags: tag:trusted                             # Disable with SET_CLIENT_TAGS_HEADER=false
```

Additional user information can be retrieved by setting `TAILSCALE_USER_LOOKUP=true` and `TAILSCALE_DEVICE_LOOKUP=true`.
The user information comes from the Devices API response, which is then used to query the Users API.
```
< X-Tailscale-User-Display-Name: james.cameron@gmail.com
< X-Tailscale-User-Id: abCGE68Dz2FG
< X-Tailscale-User-Tailnet-Id: 1234567891123456
< X-Tailscale-Device-User: james.cameron@gmail.com
< X-Tailscale-User-Created: 2024-05-11T18:35:00Z         # Disable with SET_USER_CREATED_HEADER=false
< X-Tailscale-User-Profile-Pic-Url:                      # Disable with SET_USER_PROFILE_PIC_URL_HEADER=false
< X-Tailscale-User-Role: owner                           # Disable with SET_USER_ROLE_HEADER=false
< X-Tailscale-User-Type: member                          # Disable with SET_USER_TYPE_HEADER=false
```

# Configuration

All configuration is done via environment variables.

## Required

| Variable | Description |
|---|---|
| `TAILSCALE_TAILNET` | Your tailnet name (e.g. `your-tailnet.ts.net`) |
| `TAILSCALE_OAUTH_CLIENT_ID` | OAuth client ID from the Tailscale admin console |
| `TAILSCALE_OAUTH_CLIENT_SECRET` | OAuth client secret |

## Server

| Variable | Default | Description |
|---|---|---|
| `MODE` | `http` | `http` for TCP or `sock` for Unix socket (supports systemd socket activation) |
| `HOST` | *(empty, all interfaces)* | Bind address for HTTP mode |
| `PORT` | `12999` | Listen port for HTTP mode |
| `SOCK_PATH` | `/var/run/tailscale-http-auth.sock` | Unix socket path for sock mode |

## Tailscale

| Variable | Default | Description |
|---|---|---|
| `TAILSCALE_DEVICE_LOOKUP` | `false` | Enable device info lookup via the Tailscale API |
| `TAILSCALE_USER_LOOKUP` | `false` | Enable user info lookup (requires `TAILSCALE_DEVICE_LOOKUP=true`) |
| `TAILSCALE_EXPECTED_TAILNET` | *(empty)* | If set, reject requests from devices not on this tailnet |
| `TAILSCALE_ALLOWED_TAGS` | *(empty)* | Comma-separated list of tags. Tagged devices are rejected with 403 unless they have one of these tags |

## Response headers

| Variable | Default | Description |
|---|---|---|
| `SET_DEVICE_NAME_HEADER` | `true` | Include `X-Tailscale-Device-Name` |
| `SET_CLIENT_ADDRESSES_HEADER` | `true` | Include `X-Tailscale-Device-Addresses` |
| `SET_CLIENT_OS_HEADER` | `true` | Include `X-Tailscale-Device-Os` |
| `SET_CLIENT_TAGS_HEADER` | `true` | Include `X-Tailscale-Device-Tags` (requires device lookup) |
| `SET_CLIENT_VERSION_HEADER` | `true` | Include `X-Tailscale-Device-Client-Version` (requires device lookup) |
| `SET_USER_PROFILE_PIC_URL_HEADER` | `true` | Include `X-Tailscale-User-Profile-Pic-Url` (requires user lookup) |
| `SET_USER_CREATED_HEADER` | `true` | Include `X-Tailscale-User-Created` (requires user lookup) |
| `SET_USER_TYPE_HEADER` | `true` | Include `X-Tailscale-User-Type` (requires user lookup) |
| `SET_USER_ROLE_HEADER` | `true` | Include `X-Tailscale-User-Role` (requires user lookup) |

## Cache

| Variable | Default | Description |
|---|---|---|
| `CACHE_DEVICE_EXPIRY_SECONDS` | `1800` | TTL for cached device info |
| `CACHE_GET_USER_EXPIRY_SECONDS` | `900` | TTL for cached individual user info |
| `CACHE_GET_USERS_EXPIRY_SECONDS` | `15` | TTL for cached user list |

## Logging

| Variable | Default | Description |
|---|---|---|
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `LOG_ADD_SOURCE` | `false` | Include source file/line in log output |
| `LOG_REQUEST_HEADERS_LEVEL` | `debug` | Log level at which incoming request headers are logged |
