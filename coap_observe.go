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

package lb

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/matrix-org/go-coap/v2/message"
	"github.com/matrix-org/go-coap/v2/message/codes"
	coapmux "github.com/matrix-org/go-coap/v2/mux"
)

// ObserveUpdateFn is a function which can update the long-poll request between calls.
// prevRespBody will be <nil> if this is the first call
type ObserveUpdateFn func(path string, prevRespBody []byte, req *http.Request) *http.Request

// HasUpdatedFn is a function which returns true if the responses indicate some form of change
// that the client should be notified about. `prevRespBody` will be <nil> on the first invocation.
type HasUpdatedFn func(path string, prevRespBody, respBody []byte) bool

// Observations is capable of handling CoAP OBSERVE requests and
// handles long-polling http.Handler requests on the client's behalf.
// Tokens can be extracted and used in subsequent requests by setting
// an observation update function.
type Observations struct {
	Codec         *CBORCodec
	Log           Logger
	updateFns     []ObserveUpdateFn
	hasUpdatedFn  HasUpdatedFn
	next          http.Handler
	mu            *sync.Mutex
	obs           map[string]*coapmux.Client // registration ID -> Client
	accessTokens  map[string]int             // access_token -> num observations
	lastMu        *sync.Mutex
	lastResponses map[string][]byte // remote addr + path -> last data
}

// NewObservations makes a new observations struct. `next` must be the normal HTTP handlers
// which will be called on behalf of the client. `fns` are optional path-specific update functions
// which can update a long-poll e.g extracting `next_batch` from the /sync body and using it
// as ?since= in the next request. `hasUpdatedFn` is optional and returns whether the response is meaningful or not.
// If hasUpdatedFn is missing, all responses are treated as meaningful.
func NewObservations(next http.Handler, codec *CBORCodec, hasUpdatedFn HasUpdatedFn, fns ...ObserveUpdateFn) *Observations {
	return &Observations{
		next:          next,
		mu:            &sync.Mutex{},
		updateFns:     fns,
		hasUpdatedFn:  hasUpdatedFn,
		obs:           make(map[string]*coapmux.Client),
		lastResponses: make(map[string][]byte),
		accessTokens:  make(map[string]int),
		lastMu:        &sync.Mutex{},
		Codec:         codec,
	}
}

func (o *Observations) log(format string, v ...interface{}) {
	if o.Log == nil {
		return
	}
	o.Log.Printf(format, v...)
}

// longPoll will begin long-polling on the client's behalf
func (o *Observations) longPoll(regID, path string, token []byte, req *http.Request) {
	accessToken := req.Header.Get("Authorization")
	defer func() {
		o.removeRegistration(regID, accessToken)
	}()
	var lastRespBody []byte
	var err error
	seqNum := uint32(2)
	for {
		client := o.getRegistration(regID)
		if client == nil {
			o.log("LongPoll[%s]: no client for registration - stopping long poll", regID)
			return
		}
		// modify the request according to observe functions
		// they expect to work with JSON but we send CBOR back, so let's convert the body now
		if lastRespBody != nil && lastRespBody[0] != '{' {
			lastRespBody, err = o.Codec.CBORToJSON(bytes.NewReader(lastRespBody))
			if err != nil {
				o.log("LongPoll[%s]: failed to convert CBOR to JSON from last response - stopping long poll: %s", regID, err)
			}
		}
		for _, fn := range o.updateFns {
			req = fn(path, lastRespBody, req)
		}
		// create a sink to hold the HTTP response
		w := &httpResponseSink{
			headers: make(http.Header),
		}
		// pass the request to the HTTP handler - this will block potentially
		o.next.ServeHTTP(w, req)

		// set the last response so we can update `since` tokens
		respBody, err := ioutil.ReadAll(w.body)
		if err != nil {
			o.log("LongPoll[%s]: failed to read HTTP response body - stopping long poll: %s", regID, err)
			return
		}

		if w.statusCode != 200 {
			o.log("returned code %d - stopping long poll, body: %s", w.statusCode, string(respBody))
			respCode := codes.BadGateway
			if c, ok := statusCodes[w.statusCode]; ok {
				respCode = c
			}
			o.sendResponse(*client, path, seqNum, token, respCode, nil, message.AppCBOR)
			return
		}

		if o.hasUpdatedFn != nil {
			respBodyJSON, err := o.Codec.CBORToJSON(bytes.NewReader(respBody))
			if err != nil {
				o.log("failed to convert response from CBOR to JSON: %s", err)
				// fallthrough
			} else {
				hasUpdated := o.hasUpdatedFn(path, lastRespBody, respBodyJSON)
				if !hasUpdated {
					lastRespBody = respBody
					o.log("LongPoll[%s]: response not an update, not sending", regID)
					time.Sleep(1 * time.Second)
					continue
				}
			}
		}
		backupLastRespBody := lastRespBody
		lastRespBody = respBody

		// send the response back to the caller. We trust the client will NOT call OBSERVE
		// again when they get this data, thus saving bandwidth. This will block until the client ACKs the response
		err = o.sendResponse(*client, path, seqNum, token, codes.Content, lastRespBody, message.AppCBOR)
		seqNum++
		if err != nil {
			// we will only remove this entry if there are >1 observations for this access token
			if o.safeToRemove(accessToken) {
				o.log("LongPoll[%s]: Removing registration due to error: %s", regID, err)
				return // removes registration in defer
			} else {
				o.log("LongPoll[%s]: Encountered error but keeping registration as only 1 stream is alive: %s", regID, err)
				// We have failed to send a /sync update to the client. We do not want to advance the lastRespBody else we will lose this
				// update entirely (and the client will miss messages) so revert back to the last acknowledged message.
				lastRespBody = backupLastRespBody
				// Wait a long time before retrying /sync as we know that the HS will return immediately AND we know the client is dead.
				time.Sleep(60 * time.Second)
			}
		}

		time.Sleep(1 * time.Second)
	}
}

// HandleRegistration (de)registers an observation of a resource and performs HTTP requests on behalf of the client.
//
// The response writer and message must be the OBSERVE request.
func (o *Observations) HandleRegistration(req *http.Request, w coapmux.ResponseWriter, r *coapmux.Message, register bool) {
	path, err := r.Options.Path()
	if err != nil {
		o.log("Ignoring observe request, malformed path: %s", err)
		return
	}
	regID := registrationID(w.Client(), path, r.Token)
	// Handle the OBSERVE request itself:
	if register {
		added := o.addRegistration(w.Client(), regID, req.Header.Get("Authorization"))
		if added {
			go o.longPoll(regID, path, r.Token, req)
		}
		// send ACK
		w.SetResponse(codes.Content, message.TextPlain, nil)
	} else {
		// if this is a deregister request, remove the observation and send an ACK to the client
		o.removeRegistration(regID, req.Header.Get("Authorization"))
		// send ACK
		w.SetResponse(codes.Deleted, message.TextPlain, nil)
	}
}

// HandleBlockwise MAY send back an entire response, if it can be determined that the request is part of
// a blockwise request.
func (o *Observations) HandleBlockwise(w coapmux.ResponseWriter, r *coapmux.Message) {
	path, err := r.Options.Path()
	if err != nil {
		return // no path
	}
	id := w.Client().RemoteAddr().String() + "/" + path
	o.lastMu.Lock()
	data := o.lastResponses[id]
	o.lastMu.Unlock()
	if data != nil {
		w.SetResponse(codes.Content, message.AppCBOR, bytes.NewReader(data))
	}
}

func (o *Observations) sendResponse(cc coapmux.Client, path string, seqNum uint32, token []byte, respCode codes.Code, data []byte, contentFormat message.MediaType) error {
	m := message.Message{
		Code:    respCode,
		Token:   token,
		Context: cc.Context(),
		Body:    bytes.NewReader(data),
	}
	var opts message.Options
	var buf []byte
	opts, n, err := opts.SetContentFormat(buf, contentFormat)
	if err == message.ErrTooSmall {
		buf = append(buf, make([]byte, n)...)
		opts, n, err = opts.SetContentFormat(buf, contentFormat)
	}
	if err != nil {
		return fmt.Errorf("cannot set content format to response: %w", err)
	}
	// let's the client know this message is from an OBSERVE
	opts, n, err = opts.SetObserve(buf, uint32(seqNum))
	if err == message.ErrTooSmall {
		buf = append(buf, make([]byte, n)...)
		opts, n, err = opts.SetObserve(buf, uint32(seqNum))
	}
	if err != nil {
		return fmt.Errorf("cannot set options to response: %w", err)
	}
	m.Options = opts

	// remember the last response in case it's big enough to mandate a blockwise xfer
	// in which case a separate GET request will come in for it which we will need to
	// satisfy
	id := cc.RemoteAddr().String() + "/" + path
	o.lastMu.Lock()
	o.lastResponses[id] = data
	o.lastMu.Unlock()

	// Calls to WriteMessage using a UDP client always sets the confirmable flag. We want this.
	// wait for the client to ACK this message - if they don't want to /sync anymore they will send a Reset message as per:
	//    A client that is no longer interested in receiving notifications for
	//    a resource can simply "forget" the observation.  When the server then
	//    sends the next notification, the client will not recognize the token
	//    in the message and thus will return a Reset message.  This causes the
	//    server to remove the associated entry from the list of observers.
	//    The entries in lists of observers are effectively "garbage collected"
	//    by the server.
	// https://tools.ietf.org/html/rfc7641#section-3.6
	return cc.WriteMessage(&m)
}

func (o *Observations) addRegistration(client coapmux.Client, regID, accessToken string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	// if we already have an observation running, ignore
	if _, ok := o.obs[regID]; ok {
		return false
	}
	o.obs[regID] = &client
	o.accessTokens[accessToken] += 1
	o.log("OBSERVE[%d]: add registration %s (new count=%d)", len(o.obs), regID, o.accessTokens[accessToken])
	return true
}

func (o *Observations) removeRegistration(regID, accessToken string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.obs, regID)
	o.accessTokens[accessToken] -= 1
	o.log("OBSERVE[%d]: remove registration %s (new count=%d)", len(o.obs), regID, o.accessTokens[accessToken])
}

func (o *Observations) getRegistration(regID string) *coapmux.Client {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.obs[regID]
}

func (o *Observations) safeToRemove(accessToken string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	// TODO FIXME: race condition for safeToRemove() followed by removeRegistration() without wrapping lock
	return o.accessTokens[accessToken] > 1
}

// httpResponseSink is a http.ResponseWriter which hold the HTTP response
type httpResponseSink struct {
	headers    http.Header
	body       *bytes.Reader
	statusCode int
}

func (w *httpResponseSink) Header() http.Header {
	return w.headers
}

func (w *httpResponseSink) Write(b []byte) (int, error) {
	w.body = bytes.NewReader(b)
	return len(b), nil
}

func (w *httpResponseSink) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

// The entry in the list of observers is keyed by the client endpoint
// and the token specified by the client in the request.  If an entry
// with a matching endpoint/token pair is already present in the list
// (which, for example, happens when the client wishes to reinforce its
// interest in a resource), the server MUST NOT add a new entry but MUST
// replace or update the existing one
// https://tools.ietf.org/html/rfc7641#section-4.1
func registrationID(client coapmux.Client, path string, token message.Token) string {
	return client.RemoteAddr().String() + "/" + path + "@" + token.String()
}
