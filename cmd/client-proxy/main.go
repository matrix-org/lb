package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/matrix-org/lb/mobile"
)

var (
	httpBindAddr   = flag.String("http-bind-addr", ":8008", "The HTTP listening port for the server")
	homeserverAddr = flag.String("homeserver", "", "The homeserver to forward inbound requests to, without the coaps:// e.g localhost:8008")
)

func mustInt(val string) int {
	i, err := strconv.Atoi(val)
	if err != nil {
		panic(err)
	}
	return i
}

func setConnParamsFromEnv() {
	cp := mobile.Params()
	// map of env var to what to set if it exists
	envs := map[string]func(val string){
		"LB_INSECURE_SKIP_VERIFY": func(val string) {
			cp.InsecureSkipVerify = val == "1"
		},
		"LB_FLIGHT_INTERVAL_SECS": func(val string) {
			cp.FlightIntervalSecs = mustInt(val)
		},
		"LB_HEARTBEAT_TIMEOUT_SECS": func(val string) {
			cp.HeartbeatTimeoutSecs = mustInt(val)
		},
		"LB_KEEP_ALIVE_MAX_RETRIES": func(val string) {
			cp.KeepAliveMaxRetries = mustInt(val)
		},
		"LB_KEEP_ALIVE_TIMEOUT_SECS": func(val string) {
			cp.KeepAliveTimeoutSecs = mustInt(val)
		},
		"LB_TRANSMISSION_NSTART": func(val string) {
			cp.TransmissionNStart = mustInt(val)
		},
		"LB_TRANSMISSION_ACK_TIMEOUT_SECS": func(val string) {
			cp.TransmissionACKTimeoutSecs = mustInt(val)
		},
		"LB_TRANSMISSION_MAX_RETRANSMITS": func(val string) {
			cp.TransmissionMaxRetransmits = mustInt(val)
		},
		"LB_OBSERVE_ENABLED": func(val string) {
			cp.ObserveEnabled = val == "1"
		},
		"LB_OBSERVE_BUFFER_SIZE": func(val string) {
			cp.ObserveBufferSize = mustInt(val)
		},
		"LB_OBSERVE_NO_RESPONSE_TIMEOUT_SECS": func(val string) {
			cp.ObserveNoResponseTimeoutSecs = mustInt(val)
		},
	}
	hasChanges := false
	for name, apply := range envs {
		val := os.Getenv(name)
		if val == "" {
			continue
		}
		apply(val)
		hasChanges = true
	}
	if hasChanges {
		log.Printf("detected one or more LB_ env vars\n")
		log.Printf("new config: %+v", cp)
		mobile.SetParams(cp)
	}
}

func handler(w http.ResponseWriter, req *http.Request) {
	reqURL := req.URL
	reqURL.Host = *homeserverAddr
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	var body string
	if req.Body != nil {
		bodyBytes, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errcode":"PROXY","error":"cannot read request body"}`))
			return
		}
		body = string(bodyBytes)
	}
	resp := mobile.SendRequest(
		req.Method, reqURL.String(), token, body,
	)
	if resp == nil {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"errcode":"PROXY","error":"failed to forward request to homeserver"}`))
		return
	}
	w.WriteHeader(resp.Code)
	w.Write([]byte(resp.Body))
}

func main() {
	setConnParamsFromEnv()
	flag.Parse()
	if *homeserverAddr == "" {
		log.Fatal("--homeserver must be set")
	}
	if *httpBindAddr == "" {
		log.Fatal("--http-bind-addr must be set")
	}

	http.HandleFunc("/", handler)

	srv := http.Server{
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       5 * time.Minute,
		ReadHeaderTimeout: 5 * time.Minute,
	}
	srv.Addr = *httpBindAddr
	srv.Handler = http.DefaultServeMux
	log.Printf("Listening on %v forwarding to %v", *httpBindAddr, *homeserverAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed && err != nil {
		log.Fatalf("ListenAndServe: %v", err)
	}
}
