// Copyright 2021 The Matrix.org Foundation C.I.C.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package lb

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/plgd-dev/go-coap/v2/message"
	coapmux "github.com/plgd-dev/go-coap/v2/mux"
	"github.com/plgd-dev/go-coap/v2/udp/client"
	udpmessage "github.com/plgd-dev/go-coap/v2/udp/message"
	"github.com/plgd-dev/go-coap/v2/udp/message/pool"
)

// Logger is an interface which can be satisfied to print debug logging when things go wrong.
// It is entirely optional, in which cases errors are silent.
type Logger interface {
	Printf(format string, v ...interface{})
}

// CoAPHTTP provides many ways to convert to and from HTTP/CoAP.
type CoAPHTTP struct {
	// Optional logger if you want to debug request/responses
	Log Logger
	// Which set of CoAP enum paths to use (e.g v1)
	Paths *CoAPPath
	// Custom generator for CoAP tokens. NewCoAPHTTP uses a monotonically increasing integer.
	NextToken func() message.Token
}

// NewCoAPHTTP returns various mapping functions and a wrapped HTTP handler for transparently
// mapping to and from HTTP.
//
// To aid debugging, you can set `CoAPHTTP.Log` after creation to log when things go wrong.
func NewCoAPHTTP(paths *CoAPPath) *CoAPHTTP {
	return &CoAPHTTP{
		Log:       nil,
		Paths:     paths,
		NextToken: counter,
	}
}

var count = 0

func counter() message.Token {
	count++
	buf := make([]byte, 8, 8)
	return buf[:binary.PutUvarint(buf, uint64(count))]
}

func (co *CoAPHTTP) log(format string, v ...interface{}) {
	if co.Log == nil {
		return
	}
	co.Log.Printf(format, v...)
}

// CoAPHTTPHandler transparently wraps an HTTP handler to accept and produce CoAP.
//
// `Observations` is an optional and allows the HTTP request to be observed in accordance with
// the CoAP OBSERVE specification.
func (co *CoAPHTTP) CoAPHTTPHandler(next http.Handler, ob *Observations) coapmux.Handler {
	return coapmux.HandlerFunc(func(w coapmux.ResponseWriter, r *coapmux.Message) {
		co.log("ClientAddress %v, %v\n", w.Client().RemoteAddr(), r.String())

		// we always expect clients to ask for confirmable messages as we want to replicate
		// a reliable transport. However, when blockwise xfer is used in conjunction with
		// observable resources, the RFC mandates that only the first block is sent and the
		// client should request further blocks as a separate dedicated request. This
		// is fine if we actually did REST where the GET on a resource is the complete state
		// but for /sync it really isn't. To allow callers to handle this, we'll pass
		// non-confirmable messages to the observe code and let it sort it out. Note: it's
		// non-confirmable only because the request for more blocks is piggy-backed off
		// an ACK from the first block. TODO: Actually I think the fact that it's non-con is
		// due to a go-coap bug
		if !r.IsConfirmable {
			if ob != nil {
				ob.HandleBlockwise(w, r)
			}
			return
		}
		req := co.CoAPToHTTPRequest(r.Message)
		if req == nil {
			co.log("failed to map coap request to http, ignoring")
			return
		}
		// set an access token if we know it and one hasn't been given
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			// look for one on the connection
			udpConn, ok := w.Client().ClientConn().(*client.ClientConn)
			if ok {
				token := udpConn.Context().Value(ctxValAccessToken)
				if token != nil {
					req.Header.Set("Authorization", token.(string))
				}
			}
		} else {
			//set the auth header
			udpConn, ok := w.Client().ClientConn().(*client.ClientConn)
			if ok {
				udpConn.SetContextValue(ctxValAccessToken, authHeader)
			}
		}

		//    "When included in a GET request, the Observe Option extends the GET
		//    method so it does not only retrieve a current representation of the
		//    target resource, but also requests the server to add or remove an
		//    entry in the list of observers of the resource depending on the
		//    option value.  The list entry consists of the client endpoint and the
		//    token specified by the client in the request.  Possible values are:
		//
		//      0 (register) adds the entry to the list, if not present;
		//      1 (deregister) removes the entry from the list, if present."
		//  https://tools.ietf.org/html/rfc7641#section-2
		if obs, err := r.Options.Observe(); err == nil {
			co.log("Client wants to observe resource %+v", r)
			if ob != nil {
				ob.HandleRegistration(req, w, r, obs == 0)
			}
			return
		}
		next.ServeHTTP(&coapResponseWriter{
			ResponseWriter: w,
			headers:        make(http.Header),
			logger:         co.Log,
		}, req)
	})
}

// CoAPToHTTPRequest converts a coap message into an HTTP request for http.Handler (lossy)
// Conversion expects the following coap options: (message body is not modified)
//   Uri-Host = "example.net"
//   Uri-Path = "_matrix"
//   Uri-Path = "client"
//   Uri-Path = "versions"
//   Uri-Query = "access_token=foobar"
//   Uri-Query = "limit=5"
//   => example.net/_matrix/client/versions?access_token=foobar&limit=5
func (co *CoAPHTTP) CoAPToHTTPRequest(r *message.Message) *http.Request {
	method, ok := methodCodes[r.Code]
	if !ok {
		co.log("CoAPToHTTPRequest: bad code %v", r.Code)
		return nil
	}
	// go-coap combines path segments for us
	optPath, err := r.Options.Path()
	if err != nil {
		co.log("failed to extract Uri-Path option: %s", err)
		return nil
	}
	if !strings.HasPrefix(optPath, "/") {
		optPath = "/" + optPath
	}
	path := co.Paths.CoAPPathToHTTPPath(optPath)
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	// go-coap doesn't combine queries nor does it separate key/values
	queries, err := r.Options.Queries()
	if err != nil && err != message.ErrOptionNotFound {
		co.log("failed to extract Uri-Query option: %s", err)
		return nil
	}
	query := make(url.Values)
	for _, qs := range queries {
		kvs := strings.SplitN(qs, "=", 2)
		if len(kvs) != 2 {
			co.log("ignoring malformed query string: %s", qs)
			continue
		}
		// allow repeating query params e.g ?foo=1&foo=2 => { "foo": [ "1", "2" ]}
		q := query[kvs[0]]
		q = append(q, kvs[1])
		query[kvs[0]] = q
	}
	var body []byte
	if r.Body != nil {
		body, err = ioutil.ReadAll(r.Body)
		if err != nil {
			co.log("failed to read CoAP body: %s", err)
			return nil
		}
	}
	req, err := http.NewRequest(method, "https://localhost/"+path+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		co.log("CoAPToHTTPRequest: failed to create HTTP request: %s", err)
	}

	format, err := r.Options.ContentFormat()
	if err == nil {
		contentType := contentFormatToContentType[format]
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
	}

	accessToken, _ := r.Options.GetString(OptionIDAccessToken)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	return req
}

func (co *CoAPHTTP) CoAPToHTTPResponse(r *pool.Message) *http.Response {
	resCode, ok := responseCodes[r.Code()]
	if !ok {
		co.log("CoAPToHTTPResponse: bad code %v", r.Code())
		return nil
	}
	// TODO: HTTP Response headers
	var body io.ReadCloser
	resBody := r.Body()
	if resBody != nil {
		body = ioutil.NopCloser(resBody)
	}
	res := &http.Response{
		StatusCode: resCode,
		Body:       body,
	}
	return res
}

// HTTPRequestToCoAP converts an HTTP request to a CoAP message then invokes doFn. This
// callback MUST immediately make the CoAP request and not hold a reference to the Message
// as it will be de-allocated back to a sync.Pool when the function ends. Returns an error
// if it wasn't possible to convert the HTTP request to CoAP, or if doFn returns an error.
func (co *CoAPHTTP) HTTPRequestToCoAP(req *http.Request, doFn func(*pool.Message) error) error {
	msg := pool.AcquireMessage(context.Background())
	code, ok := methodToCodes[req.Method]
	if !ok {
		return fmt.Errorf("Unknown method: %s", req.Method)
	}
	msg.SetType(udpmessage.Confirmable)
	msg.SetToken(co.NextToken())
	msg.SetCode(code)
	msg.SetPath(co.Paths.HTTPPathToCoapPath(req.URL.Path))
	queries := req.URL.Query()
	for k, vs := range queries {
		for _, v := range vs {
			msg.AddQuery(k + "=" + v)
		}
	}
	if req.Body != nil {
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("Failed to read request body: %s", err)
		} else {
			msg.SetBody(bytes.NewReader(body))
		}
	}
	cType := req.Header.Get("Content-Type")
	contentFormat, ok := contentTypeToContentFormat[cType]
	if !ok {
		contentFormat = message.AppOctets
	}
	msg.SetContentFormat(contentFormat)
	authHeader := req.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		msg.SetOptionString(OptionIDAccessToken, strings.TrimPrefix(authHeader, "Bearer "))
	}
	return doFn(msg)
}
