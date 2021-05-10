package lb

import (
	"testing"
)

func TestPaths(t *testing.T) {
	c, err := NewCoAPPath(coapv1pathMappings)
	if err != nil {
		t.Fatalf(err.Error())
	}
	cases := []struct {
		http string
		code string
	}{
		// no user params
		{
			http: "/_matrix/client/r0/sync",
			code: "/7",
		},
		// 2 user params
		{
			http: "/_matrix/client/r0/user/@frank:localhost/account_data/im.vector.setting.breadcrumbs",
			code: "/r/@frank:localhost/im.vector.setting.breadcrumbs",
		},
	}
	for _, tc := range cases {
		gotHTTP := c.CoAPPathToHTTPPath(tc.code)
		if gotHTTP != tc.http {
			t.Errorf("CoAPPathToHTTPPath with %s got %s want %s", tc.code, gotHTTP, tc.http)
		}
		gotCode := c.HTTPPathToCoapPath(tc.http)
		if gotCode != tc.code {
			t.Errorf("HTTPPathToCoapPath with %s got %s want %s", tc.http, gotCode, tc.code)
		}
	}
}

func TestPathsURLEncoding(t *testing.T) {
	c, err := NewCoAPPath(coapv1pathMappings)
	if err != nil {
		t.Fatalf(err.Error())
	}
	decodedHTTP := "/_matrix/client/r0/join/#roomIdOrAlias:localhost"
	encodedHTTP := "/_matrix/client/r0/join/%23roomIdOrAlias:localhost"
	// CoAP puts paths as URI-Path Options which don't have encoding problems
	// so we always expect this to be decoded
	code := "/L/#roomIdOrAlias:localhost"
	encodedCode := "/L/%23roomIdOrAlias:localhost"
	// If we feed it %-encodable vals they SHOULD be encoded because this HTTP path will be plopped as-is into a URL.
	// The function doesn't return path segments so the caller cannot encode the paths for themselves, we have to do it.
	got := c.CoAPPathToHTTPPath(code)
	if got != encodedHTTP {
		t.Errorf("CoAPPathToHTTPPath %s got %s want %s", code, got, decodedHTTP)
	}
	// If we feed it %xx vals they should be retained literally - basically HTTPPathToCoapPath should never encode
	// because we expect to be using http.Request.Path not http.Request.RawPath
	got = c.HTTPPathToCoapPath(encodedHTTP)
	if got != encodedCode {
		t.Errorf("HTTPPathToCoapPath %s got %s want %s", encodedCode, got, code)
	}
	// If we feed it %-encodable vals they should not be encoded - basically HTTPPathToCoapPath should never encode
	// because we expect to be using http.Request.Path not http.Request.RawPath
	got = c.HTTPPathToCoapPath(decodedHTTP)
	if got != code {
		t.Errorf("HTTPPathToCoapPath %s got %s want %s", encodedHTTP, got, code)
	}
}

func TestInvalidCodePaths(t *testing.T) {
	c, err := NewCoAPPath(coapv1pathMappings)
	if err != nil {
		t.Fatalf(err.Error())
	}
	cases := []struct {
		input  string
		output string
	}{
		// unknown coap code
		{
			input:  "/AAA",
			output: "/AAA",
		},
		// /sync but with user params (they should be dropped)
		{
			input:  "/7/extra/information",
			output: "/_matrix/client/r0/sync",
		},
		// device API with extra user params (they should be dropped)
		{
			input:  "/e/deviceid/andmore",
			output: "/_matrix/client/r0/devices/deviceid",
		},
	}
	for _, tc := range cases {
		gotHTTP := c.CoAPPathToHTTPPath(tc.input)
		if gotHTTP != tc.output {
			t.Errorf("CoAPPathToHTTPPath with %s got %s want '%s'", tc.input, gotHTTP, tc.output)
		}
	}
}
