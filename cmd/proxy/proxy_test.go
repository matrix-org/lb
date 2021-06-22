package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/matrix-org/go-coap/v2/message"
	"github.com/matrix-org/go-coap/v2/message/codes"
	udpMessage "github.com/matrix-org/go-coap/v2/udp/message"
	"github.com/matrix-org/go-coap/v2/udp/message/pool"
	"github.com/matrix-org/lb"
)

type customAddr struct {
	host string
}

func (a *customAddr) Network() string {
	return "test"
}

func (a *customAddr) String() string {
	return a.host
}

// channelPacketConn is a net.PacketConn using channels. It can only talk to one remote addr marked
// by 'raddr'.
type channelPacketConn struct {
	reads  chan []byte
	writes chan []byte
	laddr  net.Addr
	raddr  net.Addr
}

// ReadFrom reads a packet from the connection,
// copying the payload into p. It returns the number of
// bytes copied into p and the return address that
// was on the packet.
// It returns the number of bytes read (0 <= n <= len(p))
// and any error encountered. Callers should always process
// the n > 0 bytes returned before considering the error err.
// ReadFrom can be made to time out and return
// an Error with Timeout() == true after a fixed time limit;
// see SetDeadline and SetReadDeadline.
func (c *channelPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	for data := range c.reads {
		n = copy(p, data)
		return n, c.raddr, nil
	}
	return 0, nil, fmt.Errorf("read on closed chan: %+v", c.reads)
}

// WriteTo writes a packet with payload p to addr.
// WriteTo can be made to time out and return
// an Error with Timeout() == true after a fixed time limit;
// see SetDeadline and SetWriteDeadline.
// On packet-oriented connections, write timeouts are rare.
func (c *channelPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	c.writes <- p
	return len(p), nil
}
func (c *channelPacketConn) SetDeadline(t time.Time) error      { return nil }
func (c *channelPacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *channelPacketConn) SetWriteDeadline(t time.Time) error { return nil }
func (c *channelPacketConn) Close() error {
	close(c.reads)
	close(c.writes)
	return nil
}
func (c *channelPacketConn) LocalAddr() net.Addr {
	return c.laddr
}

// Test that a custom packet conn can be used to make client requests. Tested by hitting a fake /versions
func TestOutboundCustomPacketConn(t *testing.T) {
	pconn := &channelPacketConn{
		reads:  make(chan []byte),
		writes: make(chan []byte),
		laddr:  &customAddr{"local"},
		raddr:  &customAddr{"remote"},
	}
	cborCodec := lb.NewCBORCodecV1(true)
	cfg := &Config{
		ListenProxy:                  ":8091",
		CBORCodec:                    cborCodec,
		CoAPHTTP:                     lb.NewCoAPHTTP(lb.NewCoAPPathV1()),
		OutgoingFederationPacketConn: pconn,
		FederationAddrResolver: func(host string) net.Addr {
			return &customAddr{host}
		},
	}

	go func() {
		if err := RunProxyServer(cfg); err != nil {
			t.Errorf("RunProxyServer returned error: %s", err)
		}
	}()
	time.Sleep(100 * time.Millisecond) // yuck

	jsonBody := []byte(`{"versions":["0.6.0"]}`)
	cborBody, err := cborCodec.JSONToCBOR(bytes.NewBuffer(jsonBody))
	if err != nil {
		t.Fatalf("failed to convert json to cbor: %s", err)
	}

	go func() {
		// we expect a write for GET /versions
		for data := range pconn.writes {
			input := pool.AcquireMessage(context.Background())
			_, err := input.Unmarshal(data)
			if err != nil {
				t.Errorf("failed to unmarshal msg: %s", err)
				return
			}
			if input.Code() != codes.GET {
				t.Errorf("proxied wrong code, got %v want %v", input.Code(), codes.GET)
			}
			path, err := input.Options().Path()
			if err != nil {
				t.Errorf("failed to get path: %s", err)
			}
			fmt.Printf("recv %+v \n", input)

			// important that mid, token, path, type, etc are set else go-coap won't know this is
			// a response to the request
			msg := pool.AcquireMessage(context.Background())
			msg.SetCode(codes.Content)
			msg.SetContentFormat(message.TextPlain)
			msg.SetMessageID(input.MessageID())
			msg.SetToken(input.Token())
			msg.SetPath(path)
			msg.SetType(udpMessage.Acknowledgement)
			msg.SetBody(bytes.NewReader(cborBody))
			output, err := msg.Marshal()
			if err != nil {
				t.Errorf("failed to marshal output msg: %s", err)
			}
			fmt.Printf("responding %x\n", output)
			pconn.reads <- output
		}
	}()

	// send in some HTTP, expect it to be proxied
	resp, err := http.Get("http://localhost:8091/_matrix/client/versions")
	if err != nil {
		t.Fatalf("failed to GET: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Bad response status code, got %d want 200", resp.StatusCode)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read HTTP response: %s", err)
	}
	if !bytes.Equal(body, jsonBody) {
		t.Fatalf("bad response body, got %s want %s", string(body), string(jsonBody))
	}
}
