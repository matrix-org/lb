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

package lb_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/matrix-org/lb"
)

func ExampleCBORCodecV1_CBORToJSON() {
	// Test case from MSC3079
	input := `a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6`
	inputBytes, err := hex.DecodeString(input)
	if err != nil {
		panic(err)
	}
	v1 := lb.NewCBORCodecV1(true)
	output, err := v1.CBORToJSON(bytes.NewBuffer(inputBytes))
	if err != nil {
		panic(err)
	}
	fmt.Println(string(output))
	// Output:
	// {"content":{"body":"Hello World","msgtype":"m.text"},"room_id":"!foo:localhost","sender":"@alice:localhost","type":"m.room.message","unsigned":{"bool_value":true,"null_value":null}}
}

func ExampleCBORCodecV1_JSONToCBOR() {
	// Test case from MSC3079
	input := `
	{
		"type": "m.room.message",
		"content": {
		  "msgtype": "m.text",
		  "body": "Hello World"
		},
		"sender": "@alice:localhost",
		"room_id": "!foo:localhost",
		"unsigned": {
		  "bool_value": true,
		  "null_value": null
		}
	  }`
	v1 := lb.NewCBORCodecV1(true)
	output, err := v1.JSONToCBOR(bytes.NewBufferString(input))
	if err != nil {
		panic(err)
	}
	fmt.Println(hex.EncodeToString(output))
	// Output:
	// a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6
}

func ExampleCBORToJSONHandler() {
	// This handler accepts JSON and returns JSON: it knows nothing about CBOR.
	// We will give it the test case CBOR and it will return the same test case CBOR
	// by wrapping this handler in CBORToJSONHandler
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		event := struct {
			Sender  string                 `json:"sender"`
			Type    string                 `json:"type"`
			Content map[string]interface{} `json:"content"`
		}{}
		// The request body was transparently converted from CBOR to JSON for us
		if err := json.NewDecoder(req.Body).Decode(&event); err != nil {
			panic(err)
		}
		// Check the fields
		if event.Sender != "@alice:localhost" ||
			event.Type != "m.room.message" ||
			event.Content["msgtype"] != "m.text" ||
			event.Content["body"] != "Hello World" {

			w.WriteHeader(400)
			return
		}
		// we MUST tell it that we are sending back JSON before WriteHeader is called
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// This response body will transparently be converted into CBOR
		w.Write([]byte(`
		{
			"type": "m.room.message",
			"content": {
			  "msgtype": "m.text",
			  "body": "Hello World"
			},
			"sender": "@alice:localhost",
			"room_id": "!foo:localhost",
			"unsigned": {
			  "bool_value": true,
			  "null_value": null
			}
		  }`))
	})

	// This is where we call into the library, the rest of this is just setup/verification code
	// ----------------------------------------------------------------------------------
	// wrap the JSON handler and set it on the default serv mux
	http.Handle("/json_endpoint", lb.CBORToJSONHandler(handler, lb.NewCBORCodecV1(true), nil))
	// ----------------------------------------------------------------------------------

	// Test case from MSC3079
	inputData, err := hex.DecodeString(`a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6`)
	if err != nil {
		panic(err)
	}
	server := httptest.NewServer(http.DefaultServeMux)
	defer server.Close()
	res, err := http.Post(server.URL+"/json_endpoint", "application/cbor", bytes.NewBuffer(inputData))
	if err != nil {
		panic(err)
	}
	if res.StatusCode != 200 {
		panic(fmt.Sprintf("returned HTTP %d", res.StatusCode))
	}
	defer res.Body.Close()
	// cbor should have been returned
	respBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(hex.EncodeToString(respBody))
	// Output: a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6
}
