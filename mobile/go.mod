module github.com/matrix-org/lb/mobile

go 1.14

replace github.com/matrix-org/lb => ../

require (
	github.com/matrix-org/go-coap/v2 v2.0.0-20210602182203-56e40165e75e
	github.com/matrix-org/lb v0.0.0-00010101000000-000000000000
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/pion/dtls/v2 v2.0.10-0.20210502094952-3dc563b9aede
	github.com/sirupsen/logrus v1.7.0
	github.com/tidwall/gjson v1.6.7 // indirect
)
