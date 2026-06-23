package com.github.catvod.net;

import fyms.csp.Json;
import fyms.csp.RpcIO;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;

public final class OkHttp {
    private OkHttp() {}

    public static String string(String url) throws Exception {
        return request(url, "GET", new HashMap<String, String>(), "");
    }

    public static String string(String url, Map<String, String> headers) throws Exception {
        return request(url, "GET", headers, "");
    }

    public static String post(String url, Map<String, String> headers, String body) throws Exception {
        return request(url, "POST", headers, body);
    }

    private static String request(String url, String method, Map<String, String> headers, String body) throws Exception {
        String id = UUID.randomUUID().toString();
        Map<String, Object> msg = new HashMap<>();
        Map<String, Object> req = new HashMap<>();
        req.put("url", url);
        req.put("method", method);
        req.put("headers", headers == null ? new HashMap<String, String>() : headers);
        req.put("body", body == null ? "" : body);
        msg.put("type", "http_request");
        msg.put("id", id);
        msg.put("request", req);
        RpcIO.writeJsonLine(msg);

        String line;
        while ((line = RpcIO.readLine()) != null) {
            Map<String, Object> resp = Json.parseObject(line);
            if (!id.equals(String.valueOf(resp.get("id")))) {
                continue;
            }
            if (!Boolean.TRUE.equals(resp.get("ok"))) {
                throw new RuntimeException(String.valueOf(resp.get("error")));
            }
            Object text = resp.get("bodyText");
            if (text != null) {
                return String.valueOf(text);
            }
            String encoded = String.valueOf(resp.get("bodyBase64"));
            return new String(java.util.Base64.getDecoder().decode(encoded), StandardCharsets.UTF_8);
        }
        throw new RuntimeException("Go HTTP bridge 无响应");
    }
}
