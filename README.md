# Low Bandwidth Matrix

[![Go Reference](https://pkg.go.dev/badge/github.com/matrix-org/lb.svg)](https://pkg.go.dev/github.com/matrix-org/lb)

This repository implements [MSC3079](https://github.com/matrix-org/matrix-doc/pull/3079) in Go.
It also provides several command line tools to get up to speed with existing low bandwidth enabled servers.


### Mobile implementations

See [mobile](/mobile) for Android/iOS bindings.

### Command Line Tools

 - [jc](/cmd/jc): This tool can be used to convert JSON <--> CBOR.
 - [coap](/cmd/coap): This tool can be used to send a single CoAP request/response, similar to `curl`.

These can be tied together to interact with low-bandwidth enabled Matrix servers. For example:
```bash
# convert inline JSON to CBOR then pipe into coap to localhost:8008 then convert the CBOR response back to JSON and print to stdout
./jc -out "-" '{"auth":{"type":"m.login.dummy"},"username":"foo","password":"barbarbar"}' \
| ./coap -X POST -d '-' -H "Content-Type: application/cbor" -k  https://localhost:8008/_matrix/client/r0/register \
| ./jc -c2j -out '-' '-'
```