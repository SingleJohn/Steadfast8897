package okhttp3;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public class Headers {
    private final Map<String, String> headers;

    private Headers(Map<String, String> headers) {
        this.headers = new LinkedHashMap<>(headers);
    }

    public static Headers of(Map<String, String> headers) {
        if (headers == null) {
            return new Headers(new LinkedHashMap<>());
        }
        return new Headers(headers);
    }

    public Map<String, List<String>> toMultimap() {
        Map<String, List<String>> out = new LinkedHashMap<>();
        for (Map.Entry<String, String> entry : headers.entrySet()) {
            List<String> values = new ArrayList<>();
            values.add(entry.getValue());
            out.put(entry.getKey(), values);
        }
        return out;
    }

    public Map<String, String> toMap() {
        return new LinkedHashMap<>(headers);
    }
}
