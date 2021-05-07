package lb

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
)

func ExampleCBORCodecV1_CBORToJSON() {
	// Test case from MSC3079
	input := `a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6`
	inputBytes, err := hex.DecodeString(input)
	if err != nil {
		panic(err)
	}
	v1 := CBORCodecV1{
		// emit Matrix Canonical JSON so we can do literal string comparisons
		// callers will probably want this set to 'false' as it consumes more CPU to convert to canonical
		Canonical: true,
	}
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
	v1 := CBORCodecV1{
		// emit Canonical CBOR so we can do literal string comparisons
		// callers will probably want this set to 'false' as it consumes more CPU to convert to canonical
		Canonical: true,
	}
	output, err := v1.JSONToCBOR(bytes.NewBufferString(input))
	if err != nil {
		panic(err)
	}
	fmt.Println(hex.EncodeToString(output))
	// Output:
	// a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6
}

func ExampleJSONToCBORWriter() {
	responseCode := 400
	jsonResponseBody := []byte(`{"error":"something","errcode":"M_UNKNOWN"}`)
	w := httptest.NewRecorder()
	w.Body = bytes.NewBuffer(nil)

	// Initialise the writer with a ResponseWriter from ServeHTTP
	jcw := JSONToCBORWriter{
		ResponseWriter: w,
		CBORCodec: CBORCodecV1{
			// Set this to false, we just use this for asserting the CBOR output
			Canonical: true,
		},
	}
	// This MUST be set for the conversion to work and MUST be set before WriteHeader
	jcw.Header().Set("Content-Type", "application/json")
	jcw.WriteHeader(responseCode)
	_, err := jcw.Write(jsonResponseBody)
	if err != nil {
		panic(err)
	}

	// Assert the HTTP response code and response body
	if w.Code != responseCode {
		panic(fmt.Sprintf("wrong response code: %d", w.Code))
	}

	fmt.Println(hex.EncodeToString(w.Body.Bytes()))
	// Output:
	// a21866694d5f554e4b4e4f574e186769736f6d657468696e67
}

func ExampleCBORToJSONHandler() {
	// Test case from MSC3079, these two values are equivalent
	cborTestCaseHex := `a5026e6d2e726f6f6d2e6d65737361676503a2181b6b48656c6c6f20576f726c64181c666d2e74657874056e21666f6f3a6c6f63616c686f7374067040616c6963653a6c6f63616c686f737409a26a626f6f6c5f76616c7565f56a6e756c6c5f76616c7565f6`
	jsonTestCaseStr := `
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
		// we MUST tell it that we are sending back JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// This response body will transparently be converted into CBOR
		w.Write([]byte(jsonTestCaseStr))
	})
	// wrap the JSON handler and set it on the default serv mux
	http.Handle("/json_endpoint", CBORToJSONHandler(handler, CBORCodecV1{Canonical: true}))

	// Test case from MSC3079
	inputData, err := hex.DecodeString(cborTestCaseHex)
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
