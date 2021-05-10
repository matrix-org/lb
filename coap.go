package lb

import (
	"bytes"
	"net/http"

	"github.com/plgd-dev/go-coap/v2/message"
	"github.com/plgd-dev/go-coap/v2/message/codes"
	coapmux "github.com/plgd-dev/go-coap/v2/mux"
)

const ctxValAccessToken = "ctxValAccessToken"

// The CoAP Option ID corresponding to the access_token for Matrix requests
var OptionIDAccessToken = message.OptionID(256)

var methodCodes = map[codes.Code]string{
	codes.POST:   "POST",
	codes.PUT:    "PUT",
	codes.GET:    "GET",
	codes.DELETE: "DELETE",
}
var methodToCodes = map[string]codes.Code{}

func init() {
	for k, v := range methodCodes {
		methodToCodes[v] = k
	}
	for k, v := range statusCodes {
		responseCodes[v] = k
	}
	for k, v := range contentTypeToContentFormat {
		contentFormatToContentType[v] = k
	}
}

// https://tools.ietf.org/html/rfc8075#section-7
//
// +-------------------------------+----------------------------+------+
// | CoAP Response Code            | HTTP Status Code           | Note |
// +-------------------------------+----------------------------+------+
// | 2.01 Created                  | 201 Created                | 1    |
// | 2.02 Deleted                  | 200 OK                     | 2    |
// |                               | 204 No Content             | 2    |
// | 2.03 Valid                    | 304 Not Modified           | 3    |
// |                               | 200 OK                     | 4    |
// | 2.04 Changed                  | 200 OK                     | 2    |
// |                               | 204 No Content             | 2    |
// | 2.05 Content                  | 200 OK                     |      |
// | 2.31 Continue                 | N/A                        | 10   |
// | 4.00 Bad Request              | 400 Bad Request            |      |
// | 4.01 Unauthorized             | 403 Forbidden              | 5    |
// | 4.02 Bad Option               | 400 Bad Request            | 6    |
// |                               | 500 Internal Server Error  | 6    |
// | 4.03 Forbidden                | 403 Forbidden              |      |
// | 4.04 Not Found                | 404 Not Found              |      |
// | 4.05 Method Not Allowed       | 400 Bad Request            | 7    |
// |                               | 405 Method Not Allowed     | 7    |
// | 4.06 Not Acceptable           | 406 Not Acceptable         |      |
// | 4.08 Request Entity Incomplt. | N/A                        | 10   |
// | 4.12 Precondition Failed      | 412 Precondition Failed    |      |
// | 4.13 Request Ent. Too Large   | 413 Payload Too Large      | 11   |
// | 4.15 Unsupported Content-Fmt. | 415 Unsupported Media Type |      |
// | 5.00 Internal Server Error    | 500 Internal Server Error  |      |
// | 5.01 Not Implemented          | 501 Not Implemented        |      |
// | 5.02 Bad Gateway              | 502 Bad Gateway            |      |
// | 5.03 Service Unavailable      | 503 Service Unavailable    | 8    |
// | 5.04 Gateway Timeout          | 504 Gateway Timeout        |      |
// | 5.05 Proxying Not Supported   | 502 Bad Gateway            | 9    |
// +-------------------------------+----------------------------+------+
//
// 			  Table 2: CoAP-HTTP Response Code Mappings
var statusCodes = map[int]codes.Code{
	http.StatusOK:                    codes.Content,               // 200
	http.StatusBadRequest:            codes.BadRequest,            // 400
	http.StatusUnauthorized:          codes.Unauthorized,          // 401
	http.StatusForbidden:             codes.Forbidden,             // 403
	http.StatusNotFound:              codes.NotFound,              // 404
	http.StatusMethodNotAllowed:      codes.MethodNotAllowed,      // 405
	http.StatusRequestEntityTooLarge: codes.RequestEntityTooLarge, // 413
	http.StatusInternalServerError:   codes.InternalServerError,   // 500
	http.StatusBadGateway:            codes.BadGateway,            // 502
	http.StatusGatewayTimeout:        codes.GatewayTimeout,        // 504
}
var responseCodes = map[codes.Code]int{}

var contentTypeToContentFormat = map[string]message.MediaType{
	"application/json":         message.AppJSON,
	"application/cbor":         message.AppCBOR,
	"application/octet-stream": message.AppOctets,
	"text/plain":               message.TextPlain,
}
var contentFormatToContentType = map[message.MediaType]string{}

// coapResponseWriter is a http.ResponseWriter which actually writes CoAP instead (lossy)
type coapResponseWriter struct {
	coapmux.ResponseWriter
	headers    http.Header
	body       *bytes.Reader
	logger     Logger
	statusCode int
}

func (w *coapResponseWriter) Header() http.Header {
	return w.headers
}

func (w *coapResponseWriter) log(v ...interface{}) {
	if w.logger == nil {
		return
	}
	w.logger.Printf(v...)
}

func (w *coapResponseWriter) Write(b []byte) (int, error) {
	w.body = bytes.NewReader(b)

	code, ok := statusCodes[w.statusCode]
	if !ok {
		w.log("cannot map HTTP status %d to CoAP code, using codes.Empty", w.statusCode)
		code = codes.Empty
	}
	// check content-type header for media type
	// TODO: Parse mime type correctly and use the registry at https://tools.ietf.org/html/rfc7252#section-12.3
	cType := w.headers.Get("Content-Type")
	contentFormat, ok := contentTypeToContentFormat[cType]
	if !ok {
		contentFormat = message.AppOctets
	}
	// TODO: convert HTTP headers to options?
	w.ResponseWriter.SetResponse(code, contentFormat, w.body)
	return len(b), nil
}

func (w *coapResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}
