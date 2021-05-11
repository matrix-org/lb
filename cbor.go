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
	"net/http"
	"reflect"
	"sort"

	jsoniter "github.com/json-iterator/go"
)

var (
	json = jsoniter.ConfigCompatibleWithStandardLibrary
)

// jsonToCBORWriter is a wrapper around http.ResponseWriter which intercepts
// calls to http.ResponseWriter and modifies Content-Type headers and JSON responses.
// The caller can use this as a drop-in replacement when they are responding with JSON.
// NB: This writer does not support streamed responses. The Write() call MUST correspond
// to a single entire JSON object. If the writer is not used to send JSON, this writer
// simply proxies the calls through to the underlying http.ResponseWriter.
type jsonToCBORWriter struct {
	http.ResponseWriter
	*CBORCodec
	isSendingJSON bool
}

func (j *jsonToCBORWriter) WriteHeader(statusCode int) {
	if j.isSendingJSON {
		return
	}
	if j.Header().Get("Content-Type") == "application/json" {
		j.isSendingJSON = true
		j.Header().Set("Content-Type", "application/cbor")
	}
	j.ResponseWriter.WriteHeader(statusCode)
}

// Write the JSON output as CBOR - this relies on one write corresponding to an entire
// valid JSON object, which httputil does.
func (j *jsonToCBORWriter) Write(data []byte) (int, error) {
	if !j.isSendingJSON {
		return j.ResponseWriter.Write(data)
	}
	output, err := j.CBORCodec.JSONToCBOR(bytes.NewReader(data))
	if err != nil {
		return len(data), err
	}
	return j.ResponseWriter.Write(output)
}

func jsonInterfaceToCBORInterface(jsonInt interface{}, lookup map[string]int) interface{} {
	// JSON.Unmarshal maps to:
	// bool, for JSON booleans
	// float64, for JSON numbers
	// string, for JSON strings
	// []interface{}, for JSON arrays
	// map[string]interface{}, for JSON objects
	// nil for JSON null
	if jsonInt == nil {
		return nil
	}
	thing := reflect.ValueOf(jsonInt)
	switch thing.Type().Kind() {
	case reflect.Slice:
		// loop each element and recurse
		arr := jsonInt.([]interface{})
		for i, element := range arr {
			arr[i] = jsonInterfaceToCBORInterface(element, lookup)
		}
		return arr
	case reflect.Map:
		result := make(map[interface{}]interface{}) // CBOR allows numeric keys
		// loop each key
		m := jsonInt.(map[string]interface{})
		for k, v := range m {
			knum, ok := lookup[k]
			if ok {
				result[knum] = jsonInterfaceToCBORInterface(v, lookup)
			} else {
				result[k] = jsonInterfaceToCBORInterface(v, lookup)
			}
		}
		return result

	// base cases
	case reflect.Bool:
		fallthrough
	case reflect.Float64:
		fallthrough
	case reflect.String:
		return jsonInt
	default:
		panic("unknown reflect kind: " + thing.Type().Kind().String())
	}
}

func cborInterfaceToJSONInterface(cborInt interface{}, lookup map[int]string) interface{} {
	// CBOR.Unmarshal maps to:
	// CBOR booleans decode to bool.
	// CBOR positive integers decode to uint64.
	// CBOR negative integers decode to int64 (big.Int if value overflows).
	// CBOR floating points decode to float64.
	// CBOR byte strings decode to []byte.
	// CBOR text strings decode to string.
	// CBOR arrays decode to []interface{}.
	// CBOR maps decode to map[interface{}]interface{}.
	// CBOR null and undefined values decode to nil.
	// CBOR times (tag 0 and 1) decode to time.Time.
	// CBOR bignums (tag 2 and 3) decode to big.Int.
	if cborInt == nil {
		return nil
	}
	thing := reflect.ValueOf(cborInt)
	switch thing.Type().Kind() {
	case reflect.Slice:
		// loop each element and recurse
		arr := cborInt.([]interface{})
		for i, element := range arr {
			arr[i] = cborInterfaceToJSONInterface(element, lookup)
		}
		return arr
	case reflect.Map:
		result := make(map[string]interface{}) // JSON does NOT allow numeric keys
		// loop each key
		m := cborInt.(map[interface{}]interface{})
		var intKeys []int
		intMap := make(map[int]interface{})
		var strKeys []string
		for k, v := range m {
			// accept string keys
			kstr, ok := k.(string)
			if ok {
				strKeys = append(strKeys, kstr)
				continue
			}
			// accept int keys
			kint, ok := num(k)
			if ok {
				intKeys = append(intKeys, kint)
				// because we typecast from other int types, we need to
				// store the value in a dedicated map else looking them up may return <nil>
				// in the original map due to mismatched types
				intMap[kint] = v
				continue
			}
			// drop the key
		}
		sort.Ints(intKeys)
		sort.Strings(strKeys) // technically not needed but let's be deterministic
		// loop all int keys and resolve them fully
		for _, ik := range intKeys {
			// map to str key and set it
			kstr, ok := lookup[ik]
			if ok {
				result[kstr] = cborInterfaceToJSONInterface(intMap[ik], lookup)
			} else {
				result[fmt.Sprintf("%d", ik)] = cborInterfaceToJSONInterface(intMap[ik], lookup)
			}
		}
		// loop all str keys and resolve them fully: this will clobber int keys mapped to str keys if int->str
		// resolved to the same value, which is what we want
		for _, is := range strKeys {
			result[is] = cborInterfaceToJSONInterface(m[is], lookup)
		}
		return result
	default:
		return cborInt
	}
}

// num converts the input into an int if it is a number
func num(k interface{}) (kint int, ok bool) {
	ku64, ok := k.(uint64)
	if ok {
		return int(ku64), true
	}
	k64, ok := k.(int64)
	if ok {
		return int(k64), true
	}
	ki, ok := k.(int)
	if ok {
		return ki, true
	}
	return 0, false
}
