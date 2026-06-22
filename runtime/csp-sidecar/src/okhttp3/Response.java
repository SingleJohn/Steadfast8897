package okhttp3;

public class Response implements AutoCloseable {
    private final int code;
    private final Headers headers;
    private final ResponseBody body;

    public Response(int code, Headers headers, ResponseBody body) {
        this.code = code;
        this.headers = headers == null ? Headers.of(null) : headers;
        this.body = body;
    }

    public int code() {
        return code;
    }

    public Headers headers() {
        return headers;
    }

    public ResponseBody body() {
        return body;
    }

    @Override
    public void close() {
    }
}
