package lb

import (
	"bytes"
	stdjson "encoding/json"
	"reflect"
	"testing"

	jsoniter "github.com/json-iterator/go"
)

// TestJSONInterfaceToCBORInterface tests the intermediary representations can be converted from JSON to CBOR
func TestJSONInterfaceToCBORInterface(t *testing.T) {
	lookup := map[string]int{
		"one":   1,
		"two":   2,
		"three": 3,
	}
	cases := []struct {
		inputJSON string
		want      interface{}
	}{
		// Empty object
		{
			inputJSON: "{}",
			want:      map[interface{}]interface{}{},
		},
		// Primitive data types
		{
			inputJSON: `{"str":"string", "int":8, "bool":true,"null":null}`,
			want: map[interface{}]interface{}{
				"str":  "string",
				"int":  float64(8),
				"bool": true,
				"null": nil,
			},
		},
		// Nested objects
		{
			inputJSON: `{"top":{"mid":{"bot":{"k1":false}}}}`,
			want: map[interface{}]interface{}{
				"top": map[interface{}]interface{}{
					"mid": map[interface{}]interface{}{
						"bot": map[interface{}]interface{}{
							"k1": false,
						},
					},
				},
			},
		},

		// Nested arrays
		{
			inputJSON: `{"arr":["str",42.1,null,[1,2],{"k":"v"}],"other":"val"}`,
			want: map[interface{}]interface{}{
				"arr": []interface{}{
					"str", float64(42.1), nil, []interface{}{float64(1), float64(2)}, map[interface{}]interface{}{
						"k": "v",
					},
				},
				"other": "val",
			},
		},

		// Top-level arrays
		{
			inputJSON: `[42, "life"]`,
			want: []interface{}{
				float64(42), "life",
			},
		},

		// Lookup keys
		{
			// keys matching the lookup table get replaced, but not values
			inputJSON: `{"one":11,"other":"one", "nest":{"two":["three"]}}`,
			want: map[interface{}]interface{}{
				1:       float64(11),
				"other": "one",
				"nest": map[interface{}]interface{}{
					2: []interface{}{"three"},
				},
			},
		},
	}

	for _, c := range cases {
		var jsonInt interface{}
		if err := stdjson.Unmarshal([]byte(c.inputJSON), &jsonInt); err != nil {
			t.Errorf("Failed to unmarshal JSON %s", c.inputJSON)
			continue
		}
		got := jsonInterfaceToCBORInterface(jsonInt, lookup)
		if !reflect.DeepEqual(c.want, got) {
			t.Errorf("interfaces do not match:\ngot  %+v\nwant %+v", got, c.want)
		}
	}
}

// TestCBORInterfaceToJSONInterface tests that intermediary representations can be converted rom CBOR to JSON
func TestCBORInterfaceToJSONInterface(t *testing.T) {
	lookup := map[string]int{
		"one":   1,
		"two":   2,
		"three": 3,
	}
	reverseLookup := map[int]string{
		1: "one",
		2: "two",
		3: "three",
	}
	cases := []struct {
		inputJSON string
		want      interface{}
	}{
		// Empty object
		{
			inputJSON: "{}",
		},
		// Nested objects
		{
			inputJSON: `{"top":{"mid":{"bot":{"k1":false}}}}`,
		},
		// Top-level arrays
		{
			inputJSON: `[42,"life",true,null,11.1]`,
		},

		// Lookup keys
		{
			inputJSON: `{"one":11}`,
		},
	}

	jsoni := jsoniter.ConfigCompatibleWithStandardLibrary

	for _, c := range cases {
		var jsonInt interface{}
		if err := stdjson.Unmarshal([]byte(c.inputJSON), &jsonInt); err != nil {
			t.Errorf("Failed to unmarshal JSON %s - %s", c.inputJSON, err)
			continue
		}
		cborInt := jsonInterfaceToCBORInterface(jsonInt, lookup)
		jsonInt2 := cborInterfaceToJSONInterface(cborInt, reverseLookup)
		got, err := jsoni.Marshal(jsonInt2)
		if err != nil {
			t.Errorf("Failed to re-marshal JSON %s - %s", c.inputJSON, err)
			continue
		}
		if !bytes.Equal([]byte(c.inputJSON), got) {
			t.Errorf("did not pass through CBOR successfully:\ngot  %s\nwant %s", string(got), c.inputJSON)
		}
	}
}
