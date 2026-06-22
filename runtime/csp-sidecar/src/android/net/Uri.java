package android.net;

import java.net.URI;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;

public final class Uri {
    private final URI uri;

    private Uri(URI uri) {
        this.uri = uri;
    }

    public static Uri parse(String raw) {
        return new Uri(URI.create(raw));
    }

    public static String encode(String value) {
        return URLEncoder.encode(value, StandardCharsets.UTF_8);
    }

    public String getScheme() {
        return uri.getScheme();
    }

    public String getHost() {
        return uri.getHost();
    }

    public String getPath() {
        return uri.getPath();
    }

    public String getQuery() {
        return uri.getQuery();
    }

    @Override
    public String toString() {
        return uri.toString();
    }
}
