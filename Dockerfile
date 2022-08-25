FROM docker.io/golang:1-alpine AS base

RUN apk --update --no-cache add bash build-base

WORKDIR /build
COPY . /build

RUN go build -o proxy ./cmd/proxy
RUN go build -o coap ./cmd/coap
RUN go build -o jc ./cmd/jc
RUN go build -o client-proxy ./cmd/client-proxy

FROM alpine:latest

COPY --from=base /build/proxy /usr/bin
COPY --from=base /build/coap /usr/bin
COPY --from=base /build/jc /usr/bin
COPY --from=base /build/client-proxy /usr/bin

ENTRYPOINT [ "/usr/bin/proxy" ]
