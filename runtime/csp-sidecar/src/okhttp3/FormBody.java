package okhttp3;

import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

public class FormBody extends RequestBody {
    private FormBody(String body) {
        super(MediaType.parse("application/x-www-form-urlencoded"), body);
    }

    public static class Builder {
        private final List<String> pairs = new ArrayList<>();

        public Builder add(String name, String value) {
            pairs.add(encode(name) + "=" + encode(value));
            return this;
        }

        public Builder addEncoded(String name, String value) {
            pairs.add(encode(name) + "=" + (value == null ? "" : value));
            return this;
        }

        public FormBody build() {
            return new FormBody(String.join("&", pairs));
        }

        private static String encode(String value) {
            return URLEncoder.encode(value == null ? "" : value, StandardCharsets.UTF_8);
        }
    }
}
