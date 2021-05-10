### coap

This is a command line tool similar to `curl` which sends CoAP/DTLS to an arbitrary URL. CLI flags are identical to `curl`.
URLs can be either `coap://` or `http(s)://` - the tool ignores the URI scheme.

For example:
```
./coap -X POST -d '{"auth":{"type":"m.login.dummy"},"username":"foo","password":"barbarbar"}' -H "Content-Type: application/json" -k  https://localhost:8008/_matrix/client/r0/register
```

NOTE: This tool does not modify the request or response body. This makes this tool compatible with any data format: XML, JSON, CBOR, etc.
Typically though you will want to send CBOR, in which case you need to pipe the request body into this tool (as it's binary and cannot be inlined).
To do this, use `jc` first, e.g:

```
# Use ./coap -d '-' to read from stdin
./jc -out '-' '{"event_id":"$something"}' | ./coap -d '-' ....
```