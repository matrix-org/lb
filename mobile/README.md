### Mobile bindings

This contains a simple API which is capable of being automatically converted into mobile libraries (e.g Java).
For Android:
```
gomobile bind -target=android
```
Then copy `mobile-sources.jar` and `mobile.aar` to a suitable Android project.

The main API shape is:
```go
func SendRequest(method, hsURL, token, body string) *Response
```

For example, in Kotlin:

```kotlin
import okhttp3.Response
import okhttp3.Request
import mobile.Mobile

private fun init() {
    // First time setup for development
    val cp = Mobile.params()
    cp.insecureSkipVerify = true
    Mobile.setParams(cp)
}

// call this function in an okhttp3.Interceptor
// if <null> is returned then fallback to HTTP APIs
private fun doRequest(request: Request): Response? {
    val method = request.method
    val url = request.url.toString()
    val token = request.headers.get("Authorization")?.removePrefix("Bearer ")
    val body = this.stringifyRequestBody(request)
    // call out to the Go code
    val result = Mobile.sendRequest(method, url, token, body)
    if (result == null) {
        return null
    }
    return Response.Builder()
            .request(request)
            .code(result.getCode().toInt())
            .protocol(Protocol.HTTP_1_1)
            .message(result.getBody())
            .body(result.getBody().toByteArray().toResponseBody("application/json".toMediaTypeOrNull()))
            .addHeader("content-type", "application/json")
            .build()
}

private fun stringifyRequestBody(request: Request): String? {
    return try {
        val copy: Request = request.newBuilder().build()
        val buffer = Buffer()
        copy.body?.writeTo(buffer)
        buffer.readUtf8()
    } catch (e: IOException) {
        ""
    }
}
```

There are many connection parameters which can be configured, and it is important developers understand what
they do. There are sensible defaults, but this is only sensible for Element clients running over the public
internet. If you are running in a different network environment or with a different client, there may be
better configurations. The parameters are well explained in the code, along with the trade-offs of setting
them too high/low.