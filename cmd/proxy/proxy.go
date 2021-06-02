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

// Package proxy provides a way of running a low bandwidth Matrix server proxy
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/matrix-org/lb"
	piondtls "github.com/pion/dtls/v2"
	"github.com/matrix-org/go-coap/v2/dtls"
	"github.com/matrix-org/go-coap/v2/message"
	"github.com/matrix-org/go-coap/v2/message/codes"
	"github.com/matrix-org/go-coap/v2/mux"
	coapmux "github.com/matrix-org/go-coap/v2/mux"
	"github.com/matrix-org/go-coap/v2/net"
	"github.com/matrix-org/go-coap/v2/net/blockwise"
	"github.com/matrix-org/go-coap/v2/udp/client"
	udpMessage "github.com/matrix-org/go-coap/v2/udp/message"
	"github.com/matrix-org/go-coap/v2/udp/message/pool"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Config is the configuration options for the proxy
type Config struct {
	ListenDTLS   string            // :8008
	LocalAddr    string            // http://localhost:1234
	Certificates []tls.Certificate // Certs to use
	Advertise    string            // optional: Where this proxy is running publicly
	// how long to wait for the server to send a response before sending an ACK back
	// If this is too short, the proxy server will send more packets than it should (1x ACK, 1x Response)
	// and not do any piggybacking.
	// If this is too long, the proxy server may not ACK the message before the retransmit time is hit on
	// the client, causing a retransmission.
	// Default: 10s
	WaitTimeBeforeACK time.Duration
	CBORCodec         *lb.CBORCodec
	CoAPHTTP          *lb.CoAPHTTP
	KeyLogWriter      io.Writer
	Client            *http.Client
}

type handler interface {
	ServeCOAP(w client.ResponseWriter, r *message.Message, udpMsg *pool.Message)
}

type muxResponseWriter struct {
	w *client.ResponseWriter
}

func (w *muxResponseWriter) SetResponse(code codes.Code, contentFormat message.MediaType, d io.ReadSeeker, opts ...message.Option) error {
	return w.w.SetResponse(code, contentFormat, d, opts...)
}

func (w *muxResponseWriter) Client() mux.Client {
	return w.w.ClientConn().Client()
}

func forwardToLocalAddr(cfg *Config) http.HandlerFunc {
	localURL, err := url.Parse(cfg.LocalAddr)
	if err != nil {
		panic("cannot parse local addr URL: " + err.Error())
	}
	return func(w http.ResponseWriter, req *http.Request) {
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			logrus.WithError(err).Error("failed to read incoming request body")
			w.WriteHeader(500)
			w.Write([]byte(`Failed to read request body: ` + err.Error()))
			return
		}
		if req.Header.Get("Content-Type") == "application/cbor" {
			body, err = cfg.CBORCodec.CBORToJSON(bytes.NewBuffer(body))
			if err != nil {
				logrus.WithError(err).Error("failed to convert incoming request body from JSON to CBOR")
				w.WriteHeader(500)
				w.Write([]byte(`Failed to convert CBOR to JSON: ` + err.Error()))
				return
			}
		}
		reqURL := *req.URL
		reqURL.Scheme = localURL.Scheme
		reqURL.Host = localURL.Host

		newReq, err := http.NewRequest(req.Method, reqURL.String(), bytes.NewBuffer(body))
		if err != nil {
			logrus.WithError(err).Error("failed to form proxy HTTP request")
			w.WriteHeader(500)
			w.Write([]byte("failed to form corresponding HTTP request"))
			return
		}
		// copy headers
		for k, vs := range req.Header {
			for _, v := range vs {
				newReq.Header.Add(k, v)
			}
		}
		res, err := cfg.Client.Do(newReq)
		if err != nil {
			logrus.WithError(err).Error("failed to contact local address")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("Failed to contact local address"))
			return
		}
		resBody := writeResponse(cfg, res, w)
		if res.StatusCode != 200 {
			logrus.Warnf("%s %s returned %d from local address with body: %s",
				newReq.Method, reqURL.String(), res.StatusCode, string(resBody))
		} else {
			logrus.Infof("%s %s - 200 OK (%d bytes)", newReq.Method, reqURL.String(), len(resBody))
		}
	}
}

func writeResponse(cfg *Config, res *http.Response, w http.ResponseWriter) []byte {
	var resBody []byte
	if res.Body != nil {
		defer res.Body.Close()
		jsonBody, err := ioutil.ReadAll(res.Body)
		if err != nil {
			logrus.WithError(err).Error("failed to read local response body")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("Failed to read local response body"))
			return resBody
		}
		if cfg.Advertise != "" {
			keys := []string{
				`well_known.m\.homeserver.base_url`, // from login
				`m\.homeserver.base_url`,            // from well-known
			}
			for _, k := range keys {
				baseURL := gjson.GetBytes(jsonBody, k)
				if baseURL.Exists() {
					jsonBody2, err := sjson.SetBytes(jsonBody, k, cfg.Advertise)
					if err != nil {
						logrus.WithError(err).Error("failed to replace advertise URL")
					} else {
						jsonBody = jsonBody2
						logrus.Infof("Replaced homeserver base_url with %s", cfg.Advertise)
					}
				}
			}
		}
		if len(jsonBody) > 0 {
			resBody, err = cfg.CBORCodec.JSONToCBOR(bytes.NewBuffer(jsonBody))
			if err != nil {
				logrus.WithError(err).WithField("body", string(jsonBody)).Error("failed to convert response body from JSON to CBOR")
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte("Failed to convert response body from JSON to CBOR"))
				return resBody
			}
		}
	}
	for k, vs := range res.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	w.Write(resBody)
	return resBody
}

type logger struct{}

func (l *logger) Printf(format string, v ...interface{}) {
	logrus.Infof(format+"\n", v...)
}

// listenAndServeDTLS Starts a server on address and network over DTLS specified Invoke handler
// for incoming queries.
func listenAndServeDTLS(network string, addr string, config *piondtls.Config, waitACK time.Duration, handler coapmux.Handler) error {
	l, err := net.NewDTLSListener(network, addr, config)
	if err != nil {
		return err
	}
	defer l.Close()
	s := dtls.NewServer(
		dtls.WithHandlerFunc(func(w *client.ResponseWriter, r *pool.Message) {
			muxw := &muxResponseWriter{
				w: w,
			}
			muxr, err := pool.ConvertTo(r)
			if err != nil {
				return
			}
			// wait up to waitACK time before sending an ACK back.
			// If ServeCoAP has returned then we know the ACK has been sent.
			// If it is still blocking then we need to send an ACK back.
			var processed int32
			timer := time.AfterFunc(waitACK, func() {
				wasProcessed := atomic.LoadInt32(&processed)
				if wasProcessed == 0 {
					// we're still inside ServeCOAP, send an ACK back
					p, _ := r.Options().Path()
					logrus.WithField("mid", r.MessageID()).WithField("path", p).Warn(
						"ServeCOAP still running, sending ACK back",
					)
					ackMsg := pool.AcquireMessage(context.Background())
					ackMsg.SetCode(codes.Empty)
					ackMsg.SetType(udpMessage.Acknowledgement)
					ackMsg.SetMessageID(r.MessageID())
					ackErr := w.ClientConn().Session().WriteMessage(ackMsg)
					if ackErr != nil {
						logrus.WithError(ackErr).WithField("mid", r.MessageID()).Error(
							"Failed to send ACK",
						)
					}
				}
			})
			handler.ServeCOAP(muxw, &mux.Message{
				Message:        muxr,
				SequenceNumber: r.Sequence(),
				IsConfirmable:  r.Type() == udpMessage.Confirmable,
			})
			atomic.StoreInt32(&processed, 1)
			timer.Stop()
		}),
		// increase transfer time from 5s to 2min due to large inital sync responses
		dtls.WithBlockwise(true, blockwise.SZX1024, 2*time.Minute),
	)
	return s.Serve(l)
}

func RunProxyServer(cfg *Config) error {
	// run the DTLS server
	dtlsConfig := &piondtls.Config{
		Certificates: cfg.Certificates,
		KeyLogWriter: cfg.KeyLogWriter,
	}

	if cfg.Client == nil {
		cfg.Client = &http.Client{
			// Long timeout to handle long /sync requests
			Timeout: 5 * time.Minute,
		}
	}
	if cfg.WaitTimeBeforeACK == 0 {
		cfg.WaitTimeBeforeACK = 10 * time.Second
	}

	go func() {
		r := coapmux.NewRouter()
		handler := http.HandlerFunc(forwardToLocalAddr(cfg))
		observations := lb.NewSyncObservations(handler, cfg.CoAPHTTP.Paths, cfg.CBORCodec)
		observations.Log = &logger{}
		cfg.CoAPHTTP.Log = &logger{}
		r.DefaultHandle(cfg.CoAPHTTP.CoAPHTTPHandler(
			handler, observations,
		))
		logrus.Infof("Listening for DTLS on %s", cfg.ListenDTLS)
		if err := listenAndServeDTLS("udp", cfg.ListenDTLS, dtlsConfig, cfg.WaitTimeBeforeACK, r); err != nil {
			logrus.WithError(err).Panicf("Failed to ListenAndServeDTLS")
		}
	}()

	if cfg.Advertise != "" {
		logrus.Infof("Listening on %s/tcp to reverse proxy from %s to %s", cfg.ListenDTLS, cfg.Advertise, cfg.LocalAddr)
		localURL, err := url.Parse(cfg.LocalAddr)
		if err != nil {
			panic(err)
		}
		rp2 := httputil.NewSingleHostReverseProxy(localURL)
		rp := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				rp2.Director(req)
				req.Host = localURL.Host
			}}

		if err := http.ListenAndServe(cfg.ListenDTLS, rp); err != nil {
			logrus.WithError(err).Panicf("failed to ListenAndServe")
		}
	}

	select {} // block forever
}
