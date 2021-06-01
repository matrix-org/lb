#!/bin/bash -eux

# Make sure everything builds
go build ./cmd/jc
go build ./cmd/coap
go build ./cmd/proxy
(cd mobile && go build .) # don't make gomobile bindings as it takes too long
