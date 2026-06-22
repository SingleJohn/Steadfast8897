package okhttp3;

public class RequestBody {
    private final MediaType mediaType;
    private final String body;

    protected RequestBody(MediaType mediaType, String body) {
        this.mediaType = mediaType;
        this.body = body == null ? "" : body;
    }

    public static RequestBody create(MediaType mediaType, String body) {
        return new RequestBody(mediaType, body);
    }

    public String body() {
        return body;
    }

    public MediaType contentType() {
        return mediaType;
    }
}
