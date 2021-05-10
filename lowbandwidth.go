// Package lb provides an implementation of MSC3079: Low Bandwidth CS API
package lb

import (
	"bytes"
	"io/ioutil"
	"net/http"
)

// NewCBORCodecV1 creates a v1 codec capable of converting JSON <--> CBOR. If canonical is set,
// the output from this codec with be in canonical format for CBOR (RFC 7049 Section 3.9) and in
// Matrix Canonical JSON for JSON (https://matrix.org/docs/spec/appendices#canonical-json). Generally,
// you don't want to set canonical to true unless you are performing tests which need to produce a
// deterministic output (e.g sorted keys) as it consumes extra CPU.
func NewCBORCodecV1(canonical bool) *CBORCodec {
	c, err := NewCBORCodec(cborv1Keys, canonical)
	if err != nil {
		// this should never happen as the key map is static
		panic("failed to create cbor v1 codec: " + err.Error())
	}
	return c
}

// CBORToJSONHandler transparently wraps JSON http handlers to accept and produce CBOR.
// It wraps the provided `next` handler and modifies it in two ways:
//
//   - If the request header is 'application/cbor' then convert the request body to JSON
//     and overwrite the request body with the JSON, then invoke the `next` handler.
//   - Supply a wrapped http.ResponseWriter to the `next` handler which will convert
//     JSON written via Write() into CBOR, if and only if the header 'application/json' is
//     written first (before WriteHeader() is called).
//
// This is the main function users of this library should use if they wish to transparently
// handle CBOR. If users wish to handle all of MSC3079 they should use NewLowBandwidthV1.
func CBORToJSONHandler(next http.Handler, codec *CBORCodec, logger Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Content-Type") == "application/cbor" {
			body, err := codec.CBORToJSON(req.Body)
			if err != nil && logger != nil {
				logger.Printf("CBORToJSON: failed to convert - %s", err)
			}
			req.Body = ioutil.NopCloser(bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
		}
		next.ServeHTTP(&jsonToCBORWriter{
			ResponseWriter: w,
			CBORCodec:      codec,
		}, req)
	})
}

// NewCoAPPathV1 creates CoAP enum path mappings for version 1. This allows conversion
// between HTTP paths and CoAP enum paths such as:
//   /_matrix/client/r0/user/@frank:localhost/account_data/im.vector.setting.breadcrumbs
//   /r/@frank:localhost/im.vector.setting.breadcrumbs
func NewCoAPPathV1() *CoAPPath {
	p, err := NewCoAPPath(coapv1pathMappings)
	if err != nil {
		// this shouldn't be possible as the key map is static
		panic("failed to create coap v1 paths: " + err.Error())
	}
	return p
}
