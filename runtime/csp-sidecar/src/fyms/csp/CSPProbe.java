package fyms.csp;

import android.content.Context;
import com.github.catvod.crawler.Spider;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.lang.reflect.Method;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import javax.script.ScriptEngine;
import javax.script.ScriptEngineManager;

public final class CSPProbe {
    private static final String[] ANDROID_STUBS = new String[] {
        "android.text.TextUtils",
        "android.net.Uri",
        "android.util.Base64",
        "android.util.Log",
        "android.content.Context",
        "android.content.SharedPreferences"
    };
    private static final String[] CATVOD_STUBS = new String[] {
        "com.github.catvod.crawler.Spider",
        "com.github.catvod.net.OkHttp"
    };

    private CSPProbe() {}

    public static void main(String[] args) throws Exception {
        BufferedReader reader = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
        String line = reader.readLine();
        long start = System.currentTimeMillis();
        if (line == null || line.trim().isEmpty()) {
            emitResult(error("unknown", "", "空请求", "empty_request", start));
            return;
        }
        Map<String, Object> request = Json.parseObject(line);
        String className = stringValue(request.get("className"));
        String method = defaultString(stringValue(request.get("method")), "home");
        @SuppressWarnings("unchecked")
        Map<String, Object> callArgs = request.get("args") instanceof Map ? (Map<String, Object>) request.get("args") : new HashMap<>();
        try {
            Object spider = createSpider(className);
            Object data = callSpider(spider, method, callArgs);
            Map<String, Object> out = baseResult(true, method, className, start);
            out.put("data", data);
            emitResult(out);
        } catch (Throwable t) {
            Throwable root = t.getCause() != null ? t.getCause() : t;
            emitResult(error(method, className, root.toString(), errorType(root), start));
        }
    }

    private static Object createSpider(String className) throws Exception {
        if (className == null || className.trim().isEmpty()) {
            throw new IllegalArgumentException("className 为空");
        }
        ClassLoader parent = CSPProbe.class.getClassLoader();
        URL[] urls = classpathURLs();
        URLClassLoader loader = new URLClassLoader(urls, parent);
        Class<?> clazz = Class.forName(className, true, loader);
        Object instance = clazz.getDeclaredConstructor().newInstance();
        if (instance instanceof Spider) {
            ((Spider) instance).init(new Context(), "");
        } else {
            Method init = findMethod(clazz, "init", Context.class, String.class);
            if (init != null) {
                init.invoke(instance, new Context(), "");
            }
        }
        return instance;
    }

    private static Object callSpider(Object spider, String method, Map<String, Object> args) throws Exception {
        Class<?> clazz = spider.getClass();
        switch (method) {
            case "init":
                return "{\"ok\":true}";
            case "home":
            case "homeContent":
                return invoke(clazz, spider, "homeContent", new Class<?>[] { boolean.class }, new Object[] { booleanValue(args.get("filter")) });
            case "category":
            case "categoryContent":
                return invoke(clazz, spider, "categoryContent",
                    new Class<?>[] { String.class, String.class, boolean.class, HashMap.class },
                    new Object[] {
                        stringArg(args, "tid", "id", "1"),
                        stringArg(args, "pg", "page", "1"),
                        booleanValue(args.get("filter")),
                        new HashMap<String, String>()
                    });
            case "detail":
            case "detailContent":
                List<String> ids = stringListArg(args, "ids", "id");
                return invoke(clazz, spider, "detailContent", new Class<?>[] { List.class }, new Object[] { ids });
            case "search":
            case "searchContent":
                return invoke(clazz, spider, "searchContent",
                    new Class<?>[] { String.class, boolean.class },
                    new Object[] { stringArg(args, "key", "wd", ""), booleanValue(args.get("quick")) });
            case "play":
            case "playerContent":
                List<String> flags = stringListArg(args, "flags", "flag");
                return invoke(clazz, spider, "playerContent",
                    new Class<?>[] { String.class, String.class, List.class },
                    new Object[] { stringArg(args, "flag", "from", ""), stringArg(args, "id", "url", ""), flags });
            default:
                throw new UnsupportedOperationException("暂不支持的 CSP PoC method: " + method);
        }
    }

    private static Object invoke(Class<?> clazz, Object instance, String name, Class<?>[] types, Object[] args) throws Exception {
        Method method = findMethod(clazz, name, types);
        if (method == null) {
            throw new NoSuchMethodException(name);
        }
        Object value = method.invoke(instance, args);
        return value == null ? "" : value;
    }

    private static Method findMethod(Class<?> clazz, String name, Class<?>... types) {
        try {
            Method method = clazz.getMethod(name, types);
            method.setAccessible(true);
            return method;
        } catch (NoSuchMethodException ignored) {
            return null;
        }
    }

    private static URL[] classpathURLs() throws Exception {
        String[] entries = System.getProperty("java.class.path", "").split(java.io.File.pathSeparator);
        List<URL> urls = new ArrayList<>();
        for (String entry : entries) {
            if (entry != null && !entry.trim().isEmpty()) {
                urls.add(new java.io.File(entry).toURI().toURL());
            }
        }
        return urls.toArray(new URL[0]);
    }

    private static Map<String, Object> baseResult(boolean ok, String method, String className, long start) {
        Map<String, Object> out = new HashMap<>();
        out.put("ok", ok);
        out.put("method", method);
        out.put("className", className);
        out.put("durationMs", System.currentTimeMillis() - start);
        out.put("androidStubs", ANDROID_STUBS);
        out.put("catVodStubs", CATVOD_STUBS);
        out.put("networkBridge", "catvod-okhttp-stub");
        return out;
    }

    private static Map<String, Object> error(String method, String className, String message, String type, long start) {
        Map<String, Object> out = baseResult(false, method, className, start);
        out.put("error", message);
        out.put("errorType", type);
        return out;
    }

    private static void emitResult(Map<String, Object> result) {
        Map<String, Object> wrapper = new HashMap<>();
        wrapper.put("type", "result");
        wrapper.put("result", result);
        wrapper.put("durationMs", result.get("durationMs"));
        System.out.println(Json.stringify(wrapper));
        System.out.flush();
    }

    private static String errorType(Throwable t) {
        String name = t.getClass().getName();
        String message = t.getMessage() == null ? "" : t.getMessage().toLowerCase();
        if (name.contains("ClassNotFound")) {
            return "class_not_found";
        }
        if (name.contains("NoClassDefFound")) {
            return "missing_stub";
        }
        if (name.contains("NoSuchMethod")) {
            return "method_not_found";
        }
        if (message.contains("unsupported") || message.contains("暂不支持")) {
            return "unsupported";
        }
        return "runtime_error";
    }

    private static String stringArg(Map<String, Object> args, String first, String second, String fallback) {
        String value = stringValue(args.get(first));
        if (value == null || value.isEmpty()) {
            value = stringValue(args.get(second));
        }
        return value == null || value.isEmpty() ? fallback : value;
    }

    private static boolean booleanValue(Object value) {
        if (value instanceof Boolean) {
            return (Boolean) value;
        }
        return Boolean.parseBoolean(stringValue(value));
    }

    private static List<String> stringListArg(Map<String, Object> args, String listKey, String singleKey) {
        Object raw = args.get(listKey);
        List<String> out = new ArrayList<>();
        if (raw instanceof List) {
            for (Object item : (List<?>) raw) {
                String value = stringValue(item);
                if (value != null && !value.isEmpty()) {
                    out.add(value);
                }
            }
        }
        if (out.isEmpty()) {
            String single = stringValue(args.get(singleKey));
            if (single != null && !single.isEmpty()) {
                out.add(single);
            }
        }
        return out;
    }

    private static String defaultString(String value, String fallback) {
        return value == null || value.trim().isEmpty() ? fallback : value;
    }

    private static String stringValue(Object value) {
        return value == null ? "" : String.valueOf(value);
    }
}
