# Tailscale HTTP Auth for reverse proxies

Inspired by https://github.com/tailscale/tailscale/tree/main/cmd/nginx-auth, which does not support tagged devices.  

This uses the API to get more information about the client device.

https://caddyserver.com/docs/caddyfile/directives/forward_auth#forward-auth

# Quick start
Set the environment variables `TAILSCALE_TAILNET`, `TAILSCALE_OAUTH_CLIENT_ID` and `TAILSCALE_OAUTH_CLIENT_SECRET`.

Set up the authentication in your web server of choice.  
The gist of it is that you send the headers `Remote-Addr` and `Remote-Port` to the Auth service.

By default, only a whois is done to the Tailscale API.
Assuming the client IP is valid, you will get a response like:

```
< HTTP/1.1 204 No Content
< X-Tailscale-Device-Name: ipad.janky-gorilla.ts.net.
< X-Tailscale-Device-Id: 1234567891123456
< X-Tailscale-Device-Node-Id: abCGE68Dz2FG                                             # Also known as Stable ID
< X-Tailscale-Device-Addresses: 100.16.200.50, fa4a:225c:31e0:ca12:4544:aa96:6247:800b # Disable with SET_CLIENT_ADDRESSES_HEADER=false
< X-Tailscale-Device-Os: iOS                                                           # Disable with SET_CLIENT_OS_HEADER=false
```

Additional device information can be retrived by setting `TAILSCALE_DEVICE_LOOKUP=true`.  
This does a request to the Devices API.
```
< X-Tailscale-Device-Authorized: true                                                  
< X-Tailscale-Device-External: false
< X-Tailscale-Device-Client-Version: 1.80.2-t62b8bf6a0-g3c35ee987  # Disable with SET_CLIENT_VERSION_HEADER=false
< X-Tailscale-Device-Tags: tag:trusted                             # Disable with SET_CLIENT_TAGS_HEADER=false
```

Addtional user information can be retrieved by setting `TAILSCALE_USER_LOOKUP=true` and `TAILSCALE_DEVICE_LOOKUP=true`.  
We get the user information in the reponse from the Devices API, which we can then use to get additional information from the Users API.
```
< X-Tailscale-User-Display-Name: james.cameron@gmail.com
< X-Tailscale-User-Id: abCGE68Dz2FG
< X-Tailscale-User-Tailnet-Id: 1234567891123456
< X-Tailscale-User-Created: 2024-05-11T18:35:00Z         # Disable with SET_USER_CREATED_HEADER=false
< X-Tailscale-User-Profile-Pic-Url:                      # Disable with SET_USER_PROFILE_PIC_URL_HEADER=false
< X-Tailscale-User-Role: owner                           # Disable with SET_USER_ROLE_HEADER=false
< X-Tailscale-User-Type: member                          # Disable with SET_USER_TYPE_HEADER=false
```