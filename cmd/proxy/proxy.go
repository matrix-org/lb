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
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/matrix-org/go-coap/v2/coap"
	"github.com/matrix-org/go-coap/v2/dtls"
	"github.com/matrix-org/go-coap/v2/message"
	"github.com/matrix-org/go-coap/v2/message/codes"
	"github.com/matrix-org/go-coap/v2/mux"
	coapmux "github.com/matrix-org/go-coap/v2/mux"
	coapNet "github.com/matrix-org/go-coap/v2/net"
	"github.com/matrix-org/go-coap/v2/net/blockwise"
	"github.com/matrix-org/go-coap/v2/udp/client"
	udpMessage "github.com/matrix-org/go-coap/v2/udp/message"
	"github.com/matrix-org/go-coap/v2/udp/message/pool"
	"github.com/matrix-org/lb"
	"github.com/matrix-org/lb/mobile"
	piondtls "github.com/pion/dtls/v2"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Config is the configuration options for the proxy
type Config struct {
	ListenDTLS             string            // UDP :8008
	ListenProxy            string            // TCP :8009
	LocalAddr              string            // Where Synapse is located: http://localhost:1234
	OutboundFederationPort int               // which UDP port will we expect to find DTLS on?
	Certificates           []tls.Certificate // Certs to use
	Advertise              string            // optional: Where this proxy is running publicly
	// how long to wait for the server to send a response before sending an ACK back
	// If this is too short, the proxy server will send more packets than it should (1x ACK, 1x Response)
	// and not do any piggybacking.
	// If this is too long, the proxy server may not ACK the message before the retransmit time is hit on
	// the client, causing a retransmission.
	// Default: 5s
	WaitTimeBeforeACK time.Duration
	AdvertiseOnHTTPS  bool // true to host the TCP reverse proxy using the certificates in Certificates
	CBORCodec         *lb.CBORCodec
	CoAPHTTP          *lb.CoAPHTTP
	KeyLogWriter      io.Writer
	Client            *http.Client
	// Optional. If set, will route federation requests via this packet conn instead of DTLS
	OutgoingFederationPacketConn net.PacketConn
	IncomingFederationPacketConn net.PacketConn
	FederationAddrResolver       func(host string) net.Addr
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
			_, _ = w.Write([]byte(`Failed to read request body: ` + err.Error()))
			return
		}
		if req.Header.Get("Content-Type") == "application/cbor" {
			body, err = cfg.CBORCodec.CBORToJSON(bytes.NewBuffer(body))
			if err != nil {
				logrus.WithError(err).Error("failed to convert incoming request body from JSON to CBOR")
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`Failed to convert CBOR to JSON: ` + err.Error()))
				return
			}
		}
		reqURL := *req.URL
		reqURL.Scheme = localURL.Scheme
		reqURL.Host = localURL.Host
		reqURL.ForceQuery = false
		reqURL.RawQuery = req.URL.RawQuery

		newReq, err := http.NewRequest(req.Method, reqURL.String(), bytes.NewBuffer(body))
		if err != nil {
			logrus.WithError(err).Error("failed to form proxy HTTP request")
			w.WriteHeader(500)
			_, _ = w.Write([]byte("failed to form corresponding HTTP request"))
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
			_, _ = w.Write([]byte("Failed to contact local address"))
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
			_, _ = w.Write([]byte("Failed to read local response body"))
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
				_, _ = w.Write([]byte("Failed to convert response body from JSON to CBOR"))
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
	_, _ = w.Write(resBody)
	return resBody
}

type logger struct{}

func (l *logger) Printf(format string, v ...interface{}) {
	logrus.Infof(format+"\n", v...)
}

// listenAndServeDTLS Starts a server on address and network over DTLS specified Invoke handler
// for incoming queries.
func listenAndServeDTLS(network string, addr string, config *piondtls.Config, waitACK time.Duration, handler coapmux.Handler) error {
	l, err := coapNet.NewDTLSListener(network, addr, config)
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

func (cfg *Config) proxyToDTLS(w http.ResponseWriter, r *http.Request) {
	logrus.Debugf("Federation proxy %s", r.URL.RequestURI())
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(500)
	}
	res := mobile.SendRequest(
		r.Method,
		fmt.Sprintf("https://%s:%d%s", r.Host, cfg.OutboundFederationPort, r.URL.RequestURI()), // TODO: make less yuck
		strings.TrimPrefix(r.Header.Get("Authorization"), "X-Matrix "),
		string(body),
		true,
	)
	if res == nil {
		logrus.Warnf("request %s failed", r.URL.RequestURI())
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.Code)
	_, _ = w.Write([]byte(res.Body))
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
		cfg.WaitTimeBeforeACK = 5 * time.Second
	}

	if cfg.ListenDTLS != "" {
		go func() {
			dtlsRouter := coapmux.NewRouter()
			handler := http.HandlerFunc(forwardToLocalAddr(cfg))
			observations := lb.NewSyncObservations(handler, cfg.CoAPHTTP.Paths, cfg.CBORCodec)
			observations.Log = &logger{}
			cfg.CoAPHTTP.Log = &logger{}
			dtlsRouter.DefaultHandle(cfg.CoAPHTTP.CoAPHTTPHandler(
				handler, observations,
			))
			logrus.Infof("Proxying inbound DTLS->HTTP on %s (ACK piggyback period: %v)", cfg.ListenDTLS, cfg.WaitTimeBeforeACK)
			if err := listenAndServeDTLS("udp", cfg.ListenDTLS, dtlsConfig, cfg.WaitTimeBeforeACK, dtlsRouter); err != nil {
				logrus.WithError(err).Panicf("Failed to ListenAndServeDTLS")
			}
		}()
	}

	if cfg.ListenProxy != "" {
		go func() {
			logrus.Infof("Proxying outbound HTTP->DTLS on %s", cfg.ListenProxy)
			logrus.Infof("Outbound federation will go to port UDP/%d", cfg.OutboundFederationPort)
			proxyRouter := http.NewServeMux()
			proxyRouter.HandleFunc("/", cfg.proxyToDTLS)

			if cfg.OutgoingFederationPacketConn != nil && cfg.FederationAddrResolver != nil {
				logrus.Infof("Custom OutgoingFederationPacketConn in use")
				mobile.SetCustomConn(cfg.OutgoingFederationPacketConn, cfg.FederationAddrResolver)
			}

			if err := http.ListenAndServe(cfg.ListenProxy, proxyRouter); err != nil {
				logrus.WithError(err).Panicf("failed to ListenAndServe for proxy")
			}
		}()
	}

	if cfg.IncomingFederationPacketConn != nil {
		go func() {
			logrus.Infof("Listening for CoAP messages on IncomingFederationPacketConn")
			activeConnectionParams := mobile.Params()
			newCfg := coap.NewConfig(
				coap.WithErrors(func(err error) {
					logrus.Warnf("coap error: %s", err)
				}),
				coap.WithHeartBeat(time.Duration(activeConnectionParams.HeartbeatTimeoutSecs)*time.Second),
				coap.WithKeepAlive(uint32(activeConnectionParams.KeepAliveMaxRetries), time.Duration(activeConnectionParams.KeepAliveTimeoutSecs)*time.Second, func(cc interface {
					Close() error
					Context() context.Context
				}) {
					return
				}),
				coap.WithTransmission(
					// FIXME? https://github.com/plgd-dev/go-coap/issues/226
					time.Duration(activeConnectionParams.TransmissionNStart)*time.Second,
					time.Duration(activeConnectionParams.TransmissionACKTimeoutSecs)*time.Second,
					activeConnectionParams.TransmissionMaxRetransmits,
				),
				coap.WithBlockwise(true, blockwise.SZX1024, 2*time.Minute),
				coap.WithLogger(&logger{}),
			)
			srv := newCfg.NewServer(cfg.IncomingFederationPacketConn)
			if err := srv.Serve(); err != nil {
				logrus.Errorf("failed to Serve: %s", err)
			}
		}()
	}

	if cfg.Advertise != "" {
		logrus.Infof("Proxying inbound HTTP on %s (forward to %s)", cfg.LocalAddr, cfg.Advertise)
		localURL, err := url.Parse(cfg.LocalAddr)
		if err != nil {
			panic(err)
		}

		rp := &httputil.ReverseProxy{
			Transport: &http.Transport{},
			Director: func(req *http.Request) {
				httputil.NewSingleHostReverseProxy(localURL).Director(req)
				logrus.Debugf("Reverse proxy %s", req.URL.String())
				req.Host = localURL.Host
			},
		}

		if cfg.AdvertiseOnHTTPS {
			logrus.Infof("Listening for TCP+TLS on %s", cfg.ListenDTLS)
			tlsServer := &http.Server{
				Addr:    cfg.ListenDTLS,
				Handler: rp,
				TLSConfig: &tls.Config{
					Certificates: cfg.Certificates,
				},
			}
			if err := tlsServer.ListenAndServeTLS("", ""); err != nil {
				logrus.WithError(err).Panicf("failed to ListenAndServeTLS")
			}
		} else {
			logrus.Infof("Listening for TCP on %s", cfg.ListenDTLS)
			if err := http.ListenAndServe(cfg.ListenDTLS, rp); err != nil {
				logrus.WithError(err).Panicf("failed to ListenAndServe")
			}
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	return fmt.Errorf("interrupted by signal")
}
