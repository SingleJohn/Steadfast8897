package okhttp3;

public class ResponseBody {
    private final String body;

    public ResponseBody(String body) {
        this.body = body == null ? "" : body;
    }

    public String string() {
        return body;
    }
}
