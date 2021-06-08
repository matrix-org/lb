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

// Package mobile contains a gomobile friendly API for creating low bandwidth mobile clients
package mobile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/matrix-org/go-coap/v2/dtls"
	"github.com/matrix-org/go-coap/v2/message"
	"github.com/matrix-org/go-coap/v2/net/blockwise"
	"github.com/matrix-org/go-coap/v2/udp/client"
	"github.com/matrix-org/go-coap/v2/udp/message/pool"
	"github.com/matrix-org/lb"
	piondtls "github.com/pion/dtls/v2"
	"github.com/sirupsen/logrus"
)

// ConnectionParams contains parameters for the entire low bandwidth stack, including DTLS, CoAP and OBSERVE.
type ConnectionParams struct {
	// If true, skips TLS certificate checks allowing this library to be used with self-signed certificates.
	// This should be false in production!
	InsecureSkipVerify bool
	// The retry rate when sending initial DTLS handshake packets. If this value is too low (lower than the
	// RTT latency) the client will be unable to establish a DTLS session with the server because the client
	// will always send another handshake before the server can respond. If this value is too high, the
	// client will take longer than required to establish a DTLS session when under high packet
	// loss network conditions.
	FlightIntervalSecs int
	// How frequently to send CoAP heartbeat packets (Empty messages). This adds bandwidth costs when no
	// traffic is flowing but is required in order to keep NAT bindings active.
	HeartbeatTimeoutSecs int
	KeepAliveMaxRetries  int
	KeepAliveTimeoutSecs int
	// The max number of simultaneous outstanding requests to the server. Important for congestion control.
	// If this value is too high then the client may flood the network with traffic and cause network problems.
	// If this value is too low then sending many requests in a row will be queued, resulting in head-of-line
	// blocking problems.
	// The CoAP RFC recommends a value of 1. https://datatracker.ietf.org/doc/html/rfc7252#section-4.8
	// XXX FIXME: This option is broken in go-coap: https://github.com/plgd-dev/go-coap/issues/226
	TransmissionNStart int
	// How long to wait after having sent a CoAP message for an ACK from the server. It is important that
	// any CoAP server sends an ACK back before this timeout is hit. Servers which implement long poll /sync
	// MUST NOT piggyback the ACK with the sync payload (that is, wait for the sync response before ACKing)
	// or else the ?timeout= value will be the ACK timeout when there are no new events. This will cause
	// clients to retransmit sync requests needlessly.
	// The CoAP RFC recommends a value of 2. https://datatracker.ietf.org/doc/html/rfc7252#section-4.8
	// If this value is too low, clients will retransmit packets needlessly when there are latency spikes.
	// If this value is too high, clients will wait too long before retransmitting when there is packet loss.
	TransmissionACKTimeoutSecs int
	// The max number of times to retry sending a CoAP packet. If this is too high it can add unecessary
	// bandwidth costs when the server is unreachable. If this is too low then the client will not handle
	// packet loss gracefully.
	// The CoAP RFC recommends a value of 4. https://datatracker.ietf.org/doc/html/rfc7252#section-4.8
	TransmissionMaxRetransmits int
	// If set, enables /sync OBSERVE requests, meaning the server will push traffic to the client
	// rather than relying on long-polling. Client implementations need no changes for this feature
	// to work. Using OBSERVE carries risks as client syncing state is now stored server-side. If the
	// server gets restarted, it will lose its OBSERVE subscriptions, meaning clients will not be pushed
	// events. Enabling this will reduce idle bandwidth costs by 50% (~160 bytes CoAP keep-alive packets
	// vs ~320 bytes with long-polling). Therefore, enabling this is most useful when used with very
	// quiet accounts, as there are no savings when the connection is not idle.
	ObserveEnabled bool
	// The channel buffer size for pushed /sync events. The client will be pushed events even without
	// calling SendRequest when OBSERVEing. This value is the size of the buffer to hold pushed events
	// until the client calls SendRequest again. Setting this high will consume more memory but ensure
	// that a flood of traffic can be buffered. Setting this too low will eventually stop the client sending
	// ACK messages back to the server, effectively acting as backpressure.
	ObserveBufferSize int
	// Clients which use long-polling will expect a regular stream of responses when calling /sync. When using
	// OBSERVE this does not happen, as traffic is ONLY sent when there is actual data. This may cause UI elements
	// to display "not connected to the server" or equivalent. To transparently fix this, this library can send
	// back fake /sync responses (with no data and the same sync token) after a certain amount of time when waiting
	// for OBSERVE data.
	ObserveNoResponseTimeoutSecs int
}

var activeConnectionParams = ConnectionParams{
	InsecureSkipVerify:   false,
	ObserveEnabled:       false,
	FlightIntervalSecs:   2,
	HeartbeatTimeoutSecs: 60,
	KeepAliveMaxRetries:  5,
	KeepAliveTimeoutSecs: 30,
	TransmissionNStart:   1,
	// proxy is 5s, 3s grace period
	TransmissionACKTimeoutSecs:   8,
	TransmissionMaxRetransmits:   4,
	ObserveBufferSize:            50,
	ObserveNoResponseTimeoutSecs: 5,
}

const (
	ctxValObserveSync     = "ctxValObserveSync"
	ctxValSentAccessToken = "ctxValSentAccessToken"
)

var dc *dtlsClients = newDTLSClients()
var cborCodec *lb.CBORCodec = lb.NewCBORCodecV1(false)
var coapHTTP *lb.CoAPHTTP = lb.NewCoAPHTTP(lb.NewCoAPPathV1())

// Params returns the current connection parameters.
func Params() *ConnectionParams {
	return &activeConnectionParams
}

// SetParams changes the connection parameters to those given. Closes all DTLS connections.
func SetParams(cp *ConnectionParams) {
	activeConnectionParams = *cp
	dc.closeAllConns()
}

// Response is a simple HTTP response
type Response struct {
	// Code is the return status code
	Code int
	// Body is the HTTP response body as a string
	Body string
}

// SendRequest sends a CoAP request to the target hsURL. All of these parameters should be treated
// as HTTP parameters (so https:// URL, JSON body), and the returned Response will also contain a
// JSON body. Returns <nil> if there was an error (e.g network error, failed conversion) in which
// case clients should use normal Matrix over HTTP to send this request.
//
// This function will block until the response is returned, or the request times out.
func SendRequest(method, hsURL, token, body string) *Response {
	logrus.Infof("DTLS SendRequest -> %s %s", method, hsURL)

	// convert JSON to CBOR
	var reqBody io.ReadSeeker
	if body != "" {
		cborBody, err := cborCodec.JSONToCBOR(bytes.NewBufferString(body))
		if err != nil {
			logrus.WithError(err).Error("Failed to convert HTTP request body from JSON to CBOR")
			return nil // send request normally
		}
		reqBody = bytes.NewReader(cborBody)
	}

	// convert HTTP params into an HTTP request
	req, err := http.NewRequest(method, hsURL, reqBody)
	if err != nil {
		logrus.WithError(err).Error("Failed to create HTTP request from params")
		return nil
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/cbor")
	}

	// fetch a DTLS client (either cached or makes a new conn)
	u, err := url.Parse(hsURL)
	if err != nil {
		logrus.WithError(err).Error("Failed to parse HS URL")
		return nil
	}
	if u.Host == "" {
		logrus.WithField("url", hsURL).Error("HS URL missing host")
		return nil
	}
	conn, err := dc.getClientForHost(u.Host)
	if err != nil {
		logrus.WithError(err).Errorf("Failed to get DTLS client for host %s", u.Host)
		return nil
	}

	// Check if we've sent an access token and set it if we need to
	sentAccessToken := conn.Context().Value(ctxValSentAccessToken)
	if sentAccessToken == nil || sentAccessToken != token {
		req.Header.Set("Authorization", "Bearer "+token)
		conn.SetContextValue(ctxValSentAccessToken, token)
	}

	// Check for /sync OBSERVE requests
	if activeConnectionParams.ObserveEnabled && strings.Contains(u.Path, "/_matrix/client/r0/sync") {
		queries := u.Query()
		since := u.Query().Get("since")
		ch := observe(conn, coapHTTP.Paths.HTTPPathToCoapPath("/_matrix/client/r0/sync"), token, queries)
		if ch == nil {
			return nil
		}
		select {
		case r := <-ch:
			logrus.Infof("Returning real /sync response")
			return r
		case <-time.After(time.Duration(activeConnectionParams.ObserveNoResponseTimeoutSecs) * time.Second):
			// return a stub response - this keeps clients happy since they think they are syncing ok
			logrus.Infof("Sending fake /sync response")
			return &Response{
				Code: 200,
				Body: `{
						"next_batch":"` + since + `",
						"account_data":{},
						"presence":{},
						"rooms":{"join":{},"peek":{},"invite":{},"leave":{}},
						"to_device":{"events":[]},
						"device_lists":{}
					}`,
			}
		}
	}

	// send the request
	var res *pool.Message
	err = coapHTTP.HTTPRequestToCoAP(req, func(msg *pool.Message) error {
		fmt.Printf("conn.Doooo %v \n", msg.String())
		res, err = conn.Do(msg)
		return err
	})
	if err != nil {
		logrus.WithError(err).Error("Failed to convert HTTP request to CoAP or to send request")

		if dc.isConnClosed(u.Host) {
			logrus.Warn("Connection is closed, re-establishing")
			conn, err = dc.getClientForHost(u.Host)
			if err != nil {
				logrus.WithError(err).Errorf("Failed to get DTLS client for host %s", u.Host)
				return nil
			}
			req.Header.Set("Authorization", "Bearer "+token)
			conn.SetContextValue(ctxValSentAccessToken, token)
			if reqBody != nil {
				_, _ = reqBody.Seek(0, 0)
				req.Body = ioutil.NopCloser(reqBody)
			}
			err = coapHTTP.HTTPRequestToCoAP(req, func(msg *pool.Message) error {
				res, err = conn.Do(msg)
				return err
			})
			if err != nil {
				logrus.WithError(err).Error("Still failed to convert HTTP request to CoAP or to send request")
				return nil
			}
			// continue parsing the response
		} else {
			return nil
		}
	}
	logrus.Infof("Got response code: %v", res.Code())

	// convert CoAP to HTTP and return the response
	httpRes := coapHTTP.CoAPToHTTPResponse(res)
	if httpRes == nil {
		return nil
	}
	// convert CBOR to JSON
	resBody, err := cborCodec.CBORToJSON(httpRes.Body)
	if err != nil {
		logrus.WithError(err).Error("Failed to read response body")
		return nil
	}

	return &Response{
		Code: httpRes.StatusCode,
		Body: string(resBody),
	}
}

func observe(conn *client.ClientConn, path, token string, queries url.Values) chan *Response {
	ctx := conn.Context()
	if ctx.Value(ctxValObserveSync) != nil {
		logrus.Infof("Observe: connection already observing; returning existing channel")
		return ctx.Value(ctxValObserveSync).(chan *Response)
	}
	// make a channel which will buffer notifications then return it
	ch := make(chan *Response, activeConnectionParams.ObserveBufferSize)
	conn.SetContextValue(ctxValObserveSync, ch)
	logrus.Infof("Observing path: %s", path)
	opts := []message.Option{
		{
			ID:    lb.OptionIDAccessToken,
			Value: []byte(token),
		},
	}
	for k, v := range queries {
		opts = append(opts, message.Option{
			ID:    message.URIQuery,
			Value: []byte(k + "=" + v[0]),
		})
	}
	_, err := conn.Observe(context.Background(), path, func(req *pool.Message) {
		// convert CoAP to HTTP and return the response
		httpRes := coapHTTP.CoAPToHTTPResponse(req)
		if httpRes == nil {
			logrus.Warnf("Observe: failed to convert CoAP to HTTP for message %+v\n", req)
			return
		}
		if httpRes.Body == nil {
			logrus.Infof("Observe: ignoring nil response body from message %+v", req)
			return
		}
		// convert CBOR to JSON
		resBody, err := cborCodec.CBORToJSON(httpRes.Body)
		if err != nil {
			logrus.WithError(err).Error("Observe: failed to read response body (CBOR->JSON)")
			return
		}
		logrus.Infof("Observe: buffering response %s", string(resBody))

		ch <- &Response{
			Code: httpRes.StatusCode,
			Body: string(resBody),
		}
	}, opts...)
	if err != nil {
		logrus.WithError(err).Errorf("Observe: failed to observe path %s", path)
		return nil
	}
	return ch
}

type dtlsClients struct {
	dtlsConfig *piondtls.Config
	conns      map[string]*client.ClientConn // host -> conn
	mu         sync.Mutex
}

func newDTLSClients() *dtlsClients {
	dtlsConfig := &piondtls.Config{
		InsecureSkipVerify: activeConnectionParams.InsecureSkipVerify,
		FlightInterval:     time.Duration(activeConnectionParams.FlightIntervalSecs) * time.Second,
	}
	return &dtlsClients{
		dtlsConfig: dtlsConfig,
		conns:      make(map[string]*client.ClientConn),
	}
}

func (c *dtlsClients) closeAllConns() {
	var conns []*client.ClientConn
	c.mu.Lock()
	for _, con := range c.conns {
		conns = append(conns, con)
	}
	c.mu.Unlock()
	for _, con := range conns {
		con.Close()
	}
	// refresh the dtls config
	c.dtlsConfig = &piondtls.Config{
		InsecureSkipVerify: activeConnectionParams.InsecureSkipVerify,
		FlightInterval:     time.Duration(activeConnectionParams.FlightIntervalSecs) * time.Second,
	}
}

func (c *dtlsClients) isConnClosed(host string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.conns[host]
	return !ok
}

func (c *dtlsClients) getClientForHost(host string) (*client.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	co, ok := c.conns[host]
	if ok {
		return co, nil
	}
	co, err := dtls.Dial(
		host, c.dtlsConfig, dtls.WithHeartBeat(time.Duration(activeConnectionParams.HeartbeatTimeoutSecs)*time.Second),
		dtls.WithKeepAlive(uint32(activeConnectionParams.KeepAliveMaxRetries), time.Duration(activeConnectionParams.KeepAliveTimeoutSecs)*time.Second, func(cc interface {
			Close() error
			Context() context.Context
		}) {
			return
		}),
		dtls.WithTransmission(
			// FIXME? https://github.com/plgd-dev/go-coap/issues/226
			time.Duration(activeConnectionParams.TransmissionNStart)*time.Second,
			time.Duration(activeConnectionParams.TransmissionACKTimeoutSecs)*time.Second,
			activeConnectionParams.TransmissionMaxRetransmits,
		),
		// long blockwise timeout to handle large sync responses which take a huge number of blocks
		dtls.WithBlockwise(true, blockwise.SZX1024, 2*time.Minute),
		dtls.WithLogger(&logger{}),
	)
	if err == nil {
		c.conns[host] = co
		// delete the entry when the connection is closed so we'll make a new one
		co.AddOnClose(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			delete(c.conns, host)
			logrus.Infof("Removed dead connection for host %s", host)
		})
	}
	return co, err
}

type logger struct{}

func (l *logger) Printf(format string, v ...interface{}) {
	logrus.Infof(format+"\n", v...)
}
