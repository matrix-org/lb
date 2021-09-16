# Client Proxy

This is a client proxy which can be used to map incoming CS API HTTP requests to CoAP. This proxy
does not support arbitrary homeserver selection: it must be pre-configured to a fixed homeserver.
This proxy can then sit alongside a client to make outbound CoAP requests on the client's behalf.

## Running

```
go build ./cmd/client-proxy
./client-proxy -http-bind-addr :8008 -homeserver "example.com:8008"
```

There are sensible defaults, but they can be overridden using environment variables. The following
options are exposed (see https://pkg.go.dev/github.com/matrix-org/lb/mobile#ConnectionParams for documentation):
```
# integer options represented like FOO=42
# boolean options represented as FOO=1 (true) and FOO=0 (false), unset does not mean false (depends on the sensible default)
LB_INSECURE_SKIP_VERIFY bool
LB_FLIGHT_INTERVAL_SECS int
LB_HEARTBEAT_TIMEOUT_SECS int
LB_KEEP_ALIVE_MAX_RETRIES int
LB_KEEP_ALIVE_TIMEOUT_SECS int
LB_TRANSMISSION_NSTART int
LB_TRANSMISSION_ACK_TIMEOUT_SECS int
LB_TRANSMISSION_MAX_RETRANSMITS int
LB_OBSERVE_ENABLED bool
LB_OBSERVE_BUFFER_SIZE int
LB_OBSERVE_NO_RESPONSE_TIMEOUT_SECS int
```