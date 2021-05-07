package lb

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"

	jsoniter "github.com/json-iterator/go"
	"github.com/matrix-org/util"
)

var (
	json = jsoniter.ConfigCompatibleWithStandardLibrary
)

// CBORCodec is an interface capable of mapping CBOR <--> JSON
type CBORCodec interface {
	// CBORToJSON converts a CBOR byte stream into JSON
	CBORToJSON(input io.Reader) ([]byte, error)
	// JSONToCBOR converts a JSON bytes stream into CBOR
	JSONToCBOR(input io.Reader) ([]byte, error)
}

// JSONToCBORWriter is a wrapper around http.ResponseWriter which intercepts
// calls to http.ResponseWriter and modifies Content-Type headers and JSON responses.
// The caller can use this as a drop-in replacement when they are responding with JSON.
// NB: This writer does not support streamed responses. The Write() call MUST correspond
// to a single entire JSON object. If the writer is not used to send JSON, this writer
// simply proxies the calls through to the underlying http.ResponseWriter.
type JSONToCBORWriter struct {
	http.ResponseWriter
	CBORCodec
	isSendingJSON bool
}

func (j *JSONToCBORWriter) WriteHeader(statusCode int) {
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
func (j *JSONToCBORWriter) Write(data []byte) (int, error) {
	if !j.isSendingJSON {
		return j.ResponseWriter.Write(data)
	}
	output, err := j.CBORCodec.JSONToCBOR(bytes.NewReader(data))
	if err != nil {
		return len(data), err
	}
	return j.ResponseWriter.Write(output)
}

// CBORToJSONHandler returns an HTTP handler which converts CBOR request bodies
// into JSON request bodies then invokes the next HTTP handler. The next handler
// MUST return JSON which will then be converted back into CBOR by writing to
// the http.ResponseWriter (JSONToCBORWriter)
func CBORToJSONHandler(next http.Handler, codec CBORCodec) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Content-Type") == "application/cbor" {
			body, err := codec.CBORToJSON(req.Body)
			if err != nil {
				util.GetLogger(req.Context()).Warnf("CBORToJSON: failed to convert - %s", err)
			}
			req.Body = ioutil.NopCloser(bytes.NewBuffer(body))
		}
		next.ServeHTTP(&JSONToCBORWriter{
			ResponseWriter: w,
			CBORCodec:      codec,
		}, req)
	})
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
		for k, v := range m {
			kint, ok := num(k)
			if ok {
				kstr, ok := lookup[kint]
				if ok {
					result[kstr] = cborInterfaceToJSONInterface(v, lookup)
				} else {
					result[fmt.Sprintf("%d", kint)] = cborInterfaceToJSONInterface(v, lookup)
				}
			} else {
				var kstr string
				kstr, ok = k.(string)
				if !ok {
					// take the string representation
					kstringer, ok := k.(fmt.Stringer)
					if !ok {
						panic(fmt.Sprintf("cannot represent CBOR key as string! key: %s val: %s", k, v))
					}
					kstr = kstringer.String()
				}
				result[kstr] = cborInterfaceToJSONInterface(v, lookup)
			}
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