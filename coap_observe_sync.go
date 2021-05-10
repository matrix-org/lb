package lb

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// NewSyncObservations returns an Observations capable of processing Matrix /sync requests
func NewSyncObservations(next http.Handler, c *CoAPPath, codec *CBORCodec) *Observations {
	return NewObservations(next, codec, func(path string, prev, curr []byte) bool {
		path = c.CoAPPathToHTTPPath(path)
		if strings.HasPrefix(path, "/") {
			path = path[1:]
		}
		if path != "_matrix/client/r0/sync" {
			return true
		}
		if prev == nil && curr != nil {
			return true
		}
		// if there are different tokens then there has been an update
		p := gjson.GetBytes(prev, "next_batch")
		c := gjson.GetBytes(curr, "next_batch")
		return !(p.Str == c.Str)

	}, func(path string, prevRespBody []byte, req *http.Request) *http.Request {
		path = c.CoAPPathToHTTPPath(path)
		if strings.HasPrefix(path, "/") {
			path = path[1:]
		}
		if path != "_matrix/client/r0/sync" {
			return req
		}
		r := gjson.GetBytes(prevRespBody, "next_batch")
		if !r.Exists() {
			return req
		}
		u := req.URL
		vals := u.Query()
		vals.Set("since", r.Str)
		vals.Set("timeout", "10000") // 10s timeout
		u.RawQuery = vals.Encode()
		req.URL = u
		return req
	})
}
