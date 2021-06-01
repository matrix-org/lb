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
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/matrix-org/lb"
	piondtls "github.com/pion/dtls/v2"
	"github.com/plgd-dev/go-coap/v2/dtls"
	"github.com/plgd-dev/go-coap/v2/udp/message/pool"
)

var (
	flagMethod   string
	flagData     string
	flagInsecure bool
	flagVerbose  bool
	flagInclude  bool
	flagHeaders  stringFlags
)

type stringFlags []string

func (i *stringFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *stringFlags) Set(value string) error {
	*i = append(*i, strings.TrimSpace(value))
	return nil
}

func init() {
	flag.StringVar(&flagMethod, "request", "GET", "HTTP Method")
	flag.StringVar(&flagMethod, "X", "GET", "HTTP Method (shorthand of --request)")
	flag.StringVar(&flagData, "data", "", "HTTP request binary body. If you start the data with the letter @, "+
		"the rest should be a file name to read the data from, or - if you want coap to read the data from stdin.")
	flag.StringVar(&flagData, "d", "", "HTTP request binary body (shorthand of --data)")
	flag.BoolVar(&flagInsecure, "insecure", false, "Skip TLS checks")
	flag.BoolVar(&flagInsecure, "k", false, "Skip TLS checks (shorthand of --insecure)")
	flag.BoolVar(&flagInclude, "include", false, "Include HTTP response headers")
	flag.BoolVar(&flagInclude, "i", false, "Include HTTP response headers (shorthand of --include)")
	flag.BoolVar(&flagVerbose, "verbose", false, "Verbose mode")
	flag.BoolVar(&flagVerbose, "v", false, "Verbose mode (shorthand of --verbose)")
	flag.Var(&flagHeaders, "header", "HTTP Header")
	flag.Var(&flagHeaders, "H", "HTTP Header (shorthand of --header)")
}

func makeHTTPRequestFromFlags(targetURL string) *http.Request {
	var reqBody io.Reader
	if flagData == "-" {
		reqBody = os.Stdin
	} else if strings.HasPrefix(flagData, "@") {
		f, err := os.Open(flagData[1:])
		if err != nil {
			log.Printf("FATAL reading request file: %s\n", err.Error())
			os.Exit(1)
		}
		reqBody = f
		defer f.Close()
	} else {
		reqBody = bytes.NewBufferString(flagData)
	}

	req, err := http.NewRequest(flagMethod, targetURL, reqBody)
	if err != nil {
		log.Printf("FATAL making request: %s\n", err.Error())
		os.Exit(1)
	}
	for _, h := range flagHeaders {
		segments := strings.SplitN(h, ":", 2)
		if len(segments) != 2 {
			log.Printf("FATAL request header malformed (needs :) %s\n", h)
			os.Exit(1)
		}
		req.Header.Set(strings.TrimSpace(segments[0]), strings.TrimSpace(segments[1]))
	}
	return req
}

func verbosePrintRequest(req *http.Request) {
	if !flagVerbose {
		return
	}
	data, err := httputil.DumpRequest(req, true)
	if err != nil {
		log.Printf("FATAL dumping request: %s\n", err.Error())
		os.Exit(1)
	}
	fmt.Printf("> %s\n\n", strings.ReplaceAll(string(data), "\n", "\n> "))
}

func printResponse(res *http.Response) {
	var body []byte
	var err error
	if res.Body != nil {
		defer res.Body.Close()
		body, err = ioutil.ReadAll(res.Body)
		if err != nil {
			log.Printf("FATAL reading response body: %s\n", err.Error())
			os.Exit(1)
		}
	}
	if flagVerbose || flagInclude {
		data, err := httputil.DumpResponse(res, false)
		if err != nil {
			log.Printf("FATAL dumping response: %s\n", err.Error())
			os.Exit(1)
		}
		if flagVerbose {
			fmt.Printf("< %s\n\n", strings.ReplaceAll(string(data), "\n", "\n< "))
		} else {
			fmt.Printf("%s", string(data))
		}
	}
	fmt.Printf("%s", string(body))
}

func mainDTLS(targetURL string, keyLogWriter io.Writer) {
	req := makeHTTPRequestFromFlags(targetURL)
	verbosePrintRequest(req)
	turl, err := url.Parse(targetURL)
	if err != nil {
		log.Printf("FATAL: target url is invalid %s : %s", targetURL, err)
		os.Exit(1)
	}
	dtlsConfig := &piondtls.Config{
		InsecureSkipVerify: flagInsecure,
		KeyLogWriter:       keyLogWriter,
	}
	co, err := dtls.Dial(turl.Host, dtlsConfig)
	if err != nil {
		log.Printf("FATAL: DTLS failed to dial UDP addr %s", err)
		os.Exit(1)
	}

	// make the low bandwidth mapping
	lbcoap := lb.NewCoAPHTTP(lb.NewCoAPPathV1())

	var coapres *pool.Message
	err = lbcoap.HTTPRequestToCoAP(req, func(msg *pool.Message) error {
		coapres, err = co.Do(msg)
		return err
	})
	if err != nil {
		log.Printf("FATAL: Failed to perform CoAP request: %s", err)
		os.Exit(1)
	}
	defer pool.ReleaseMessage(coapres)

	httpRes := lbcoap.CoAPToHTTPResponse(coapres)
	if httpRes == nil {
		log.Printf("FATAL: cannot convert CoAP to HTTP")
		os.Exit(1)
	}
	printResponse(httpRes)
	if httpRes.StatusCode >= 300 || httpRes.StatusCode < 200 {
		os.Exit(1)
	}
}

func main() {
	flag.Parse()
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of coap:\n")
		flag.PrintDefaults()
		fmt.Println("Example:                     ./coap -X POST -d '{}' -k https://localhost:8008/_matrix/client/r0/register")
		fmt.Println("Example (stdin): echo '{}' | ./coap -X POST -d '-' -k https://localhost:8008/_matrix/client/r0/register")
		fmt.Println("Example (file):              ./coap -X POST -d '@empty.json' -k https://localhost:8008/_matrix/client/r0/register")
		fmt.Println("Also supports the environment variable SSLKEYLOGFILE= to write session secrets for decrypting DTLS traffic in Wireshark")
	}

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}
	flagURL := flag.Arg(0)

	var keyLogWriter io.Writer
	var err error
	if keylogfile := os.Getenv("SSLKEYLOGFILE"); keylogfile != "" {
		keyLogWriter, err = os.OpenFile(keylogfile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
	}
	mainDTLS(flagURL, keyLogWriter)
}
