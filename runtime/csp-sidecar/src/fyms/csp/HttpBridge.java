package fyms.csp;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;
import okhttp3.Headers;
import okhttp3.Request;
import okhttp3.Response;
import okhttp3.ResponseBody;

public final class HttpBridge {
    private static final BufferedReader READER = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));

    private HttpBridge() {}

    public static synchronized Response execute(Request request) throws IOException {
        String id = UUID.randomUUID().toString();
        Map<String, Object> payload = new HashMap<>();
        Map<String, Object> req = new LinkedHashMap<>();
        req.put("url", request.url());
        req.put("method", request.method());
        Map<String, String> headers = request.headers().toMap();
        if (request.body() != null && request.body().contentType() != null && !hasHeader(headers, "Content-Type")) {
            headers.put("Content-Type", request.body().contentType().toString());
        }
        req.put("headers", headers);
        req.put("body", request.body() == null ? "" : request.body().body());
        payload.put("type", "http_request");
        payload.put("id", id);
        payload.put("request", req);
        System.out.println(Json.stringify(payload));
        System.out.flush();

        String line;
        while ((line = READER.readLine()) != null) {
            Map<String, Object> resp = Json.parseObject(line);
            if (!id.equals(String.valueOf(resp.get("id")))) {
                continue;
            }
            if (!Boolean.TRUE.equals(resp.get("ok"))) {
                throw new IOException(String.valueOf(resp.get("error")));
            }
            int status = intValue(resp.get("status"), 200);
            @SuppressWarnings("unchecked")
            Map<String, String> responseHeaders = resp.get("headers") instanceof Map ? (Map<String, String>) resp.get("headers") : new LinkedHashMap<>();
            String body = String.valueOf(resp.get("bodyText"));
            if (body == null || "null".equals(body)) {
                String encoded = String.valueOf(resp.get("bodyBase64"));
                body = new String(Base64.getDecoder().decode(encoded), StandardCharsets.UTF_8);
            }
            return new Response(status, Headers.of(responseHeaders), new ResponseBody(body));
        }
        throw new IOException("Go HTTP bridge 无响应");
    }

    private static int intValue(Object value, int fallback) {
        if (value instanceof Number) {
            return ((Number) value).intValue();
        }
        try {
            return Integer.parseInt(String.valueOf(value));
        } catch (Exception ignored) {
            return fallback;
        }
    }

    private static boolean hasHeader(Map<String, String> headers, String name) {
        for (String key : headers.keySet()) {
            if (key.equalsIgnoreCase(name)) {
                return true;
            }
        }
        return false;
    }
}
