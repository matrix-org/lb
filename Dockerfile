FROM docker.io/golang:1.16-alpine AS base

RUN apk --update --no-cache add bash build-base

WORKDIR /build
COPY . /build

RUN CGO_ENABLED=0 go build -ldflags="-extldflags=-static" -o proxy ./cmd/proxy
RUN CGO_ENABLED=0 go build -ldflags="-extldflags=-static" -o coap ./cmd/coap
RUN CGO_ENABLED=0 go build -ldflags="-extldflags=-static" -o jc ./cmd/jc

FROM alpine:latest

COPY --from=base /build/proxy /usr/bin
COPY --from=base /build/coap /usr/bin
COPY --from=base /build/jc /usr/bin

ENTRYPOINT [ "/usr/bin/proxy" ]
