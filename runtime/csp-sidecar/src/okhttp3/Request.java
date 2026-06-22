package okhttp3;

import java.util.LinkedHashMap;
import java.util.Map;

public class Request {
    private final String url;
    private final String method;
    private final Headers headers;
    private final RequestBody body;
    private final Object tag;

    private Request(Builder builder) {
        this.url = builder.url;
        this.method = builder.method;
        this.headers = Headers.of(builder.headers);
        this.body = builder.body;
        this.tag = builder.tag;
    }

    public String url() {
        return url;
    }

    public String method() {
        return method;
    }

    public Headers headers() {
        return headers;
    }

    public RequestBody body() {
        return body;
    }

    public Object tag() {
        return tag;
    }

    public static class Builder {
        private String url;
        private String method = "GET";
        private final Map<String, String> headers = new LinkedHashMap<>();
        private RequestBody body;
        private Object tag;

        public Builder url(String url) {
            this.url = url;
            return this;
        }

        public Builder addHeader(String name, String value) {
            headers.put(name, value);
            return this;
        }

        public Builder headers(Headers headers) {
            this.headers.clear();
            if (headers != null) {
                this.headers.putAll(headers.toMap());
            }
            return this;
        }

        public Builder post(RequestBody body) {
            this.method = "POST";
            this.body = body;
            return this;
        }

        public Builder tag(Object tag) {
            this.tag = tag;
            return this;
        }

        public Request build() {
            return new Request(this);
        }
    }
}
