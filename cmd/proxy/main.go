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

package main

import (
	"crypto/tls"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/matrix-org/lb"
	"github.com/sirupsen/logrus"
)

var (
	dtlsBindAddr  = flag.String("dtls-bind-addr", ":8008", "The DTLS UDP listening port for the server")
	proxyBindAddr = flag.String("proxy-bind-addr", "", "The HTTP server to act as a transparent proxy for outbound requests")
	localAddr     = flag.String("local", "", "The HTTP server to forward inbound CoAP requests to e.g http://localhost:8008")
	advertise     = flag.String("advertise", "",
		"Optional: the public address of this proxy. If set, sniffs logins/registrations for homeserver discovery information and replaces the base_url with this advertising address. "+
			"This is useful when the local server is not on the same machine as the proxy.")
	certFile = flag.String("tls-cert", "", "The PEM formatted X509 certificate to use for TLS")
	keyFile  = flag.String("tls-key", "", "The PEM private key to use for TLS")
)

func main() {
	flag.Parse()

	var certs []tls.Certificate
	var err error
	if *certFile != "" && *keyFile != "" {
		certs = make([]tls.Certificate, 1)
		certs[0], err = tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			logrus.WithError(err).Panicf("failed to load TLS certificate")
		}
	} else {
		logrus.Panicf("TLS certificate/key must be set")
	}

	if *localAddr == "" {
		logrus.Panicf("Must specify HTTP local address")
	}

	var keyLogWriter io.Writer
	if keylogfile := os.Getenv("SSLKEYLOGFILE"); keylogfile != "" {
		keyLogWriter, err = os.OpenFile(keylogfile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
	}

	err = RunProxyServer(&Config{
		ListenDTLS:       *dtlsBindAddr,
		ListenProxy:      *proxyBindAddr,
		LocalAddr:        *localAddr,
		Certificates:     certs,
		KeyLogWriter:     keyLogWriter,
		Advertise:        *advertise,
		AdvertiseOnHTTPS: *advertise != "" && strings.HasPrefix(*advertise, "https://"),
		CBORCodec:        lb.NewCBORCodecV1(true),
		CoAPHTTP:         lb.NewCoAPHTTP(lb.NewCoAPPathV1()),
	})
	if err != nil {
		logrus.Panicf("RunProxyServer: %s", err)
	}
}
