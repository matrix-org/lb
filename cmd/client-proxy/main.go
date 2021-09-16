package main

import (
	"flag"

	"github.com/matrix-org/lb/mobile"
)

var (
	httpBindAddr   = flag.String("http-bind-addr", ":8008", "The HTTP listening port for the server")
	homeserverAddr = flag.String("homeserver", "", "The homeserver to forward inbound requests to e.g coaps://localhost:8008")
)

func main() {
	mobile.SendRequest("", "", "", "")
}
