## Low Bandwidth Proxy

This is a single host reverse proxy, designed to sit in front of a Synapse or Dendrite server,
to transparently enable [MSC3079: Low Bandwidth CS API](https://github.com/matrix-org/matrix-doc/blob/kegan/low-bandwidth/proposals/3079-low-bandwidth-csapi.md#msc3079-low-bandwidth-client-server-api).

```
go build ./cmd/proxy
```

### Running

Anyone can run a proxy: you do not need to be running a Homeserver for this to work. The proxy requires TLS certificates in order
to function as it needs to perform DTLS handshakes (you cannot do TLS terimation on your reverse proxy!). It is **strongly advised**
that you use ECC certificates and not RSA certificates due to the smaller certificate size.

#### With a Homeserver on the same machine

The easiest option is to bind to the same port as the Homeserver (but on UDP so there is no port clash). You should use the
same TLS certificates as the homeserver, though this is not required.

```
./proxy -local 'http://localhost:8008' \
--tls-cert lb-certificate.pem \
--tls-key lb-key.pem  \
--dtls-bind-addr :8008
```

This setup is ideal because it means clients can fallback to HTTP APIs without changing the URL at all! This is important as things
like `/_matrix/media` requests are not supported in low bandwidth mode.

#### With a Homeserver somewhere else (e.g matrix.org)

This is useful if you want to communicate with a homeserver which doesn't support low bandwidth Matrix. It saves bandwidth at the
client level, but still communicates the normal HTTP APIs at the server level. The setup is as follows:
```
+-------+   low bandwidth  +----------+   HTTP APIs  +------------+
| Phone | <--------------> | lb-proxy | <----------> | Homeserver |
+-------+                  +----------+              +------------+
```
To do this for matrix.org:
```
./proxy -local 'https://matrix-client.matrix.org' \
 --tls-cert lb-certificate.pem \
 --tls-key lb-key.pem \
 --advertise "http://public-address-for-lb-proxy:8008" \
 --dtls-bind-addr :8008
```
Note:
 - The `-local` address is set to another homeserver's CS API URL.
 - There is a new `-advertise` flag which is the public address clients should use to talk to the low bandwidth proxy.

The proxy will intercept `.well-known` responses and replace the `m.homeserver.base_url` value with the one in `-advertise` to make sure that
the client always talks to the low bandwidth proxy and never the destination server directly.

Setting `-advertise` will make the proxy listen on TCP as well as UDP in order to proxy media requests.

### Security Considerations

 - All traffic will be visible to the proxy. This is how it can intercept well-known responses and replace URLs with the proxy.
   If you do not trust the proxy, do not connect to it! DTLS traffic can be decrypted for debugging purposes by setting
   the environment variable `SSLKEYLOGFILE` when starting the proxy, which can then be fed into Wireshark.

 - For local proxies: Request headers are forwarded to the Matrix server, including `X-Forwarded-For`. Ensure your server setup has the lb-proxy at
   the same level as the Homeserver, else you may inadvertantly allow clients to spoof their IP address. For example, if your
   Homeserver blindly trusts `X-Forwarded-For` (assuming it was set by the reverse proxy like Apache), then you MUST ensure that
   the lb-proxy is behind a reverse proxy so that header can be trusted. Failing to do this will allow clients to manually set this
   header and trick the server.

 - For remote proxies: IP addresses are hidden and will always come from the proxy server IP address. The proxy does not set `X-Forwarded-For` headers,
   and even if it did, other servers would not trust it (e.g matrix.org).

 - This is not an open proxy. Traffic cannot be made to any arbitrary URL, only the one specified in `-local`.

 - There is no authentication on the proxy. Any valid matrix user can communicate with the proxy if it is accessible.
