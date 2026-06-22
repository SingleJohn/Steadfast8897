package fyms.csp;

import android.app.Application;
import android.content.Context;
import com.github.catvod.crawler.Spider;
import com.googlecode.d2j.dex.Dex2jar;
import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Enumeration;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.jar.JarEntry;
import java.util.jar.JarFile;

public final class CSPProbe {
    private static final String[] ANDROID_STUBS = new String[] {
        "android.text.TextUtils",
        "android.net.Uri",
        "android.util.Base64",
        "android.util.Log",
        "android.app.Application",
        "android.app.Activity",
        "android.app.AlertDialog",
        "android.content.Context",
        "android.content.ComponentName",
        "android.content.DialogInterface",
        "android.content.Intent",
        "android.content.pm.ApplicationInfo",
        "android.content.pm.PackageManager",
        "android.content.pm.PackageInfo",
        "android.content.SharedPreferences",
        "android.graphics.Bitmap",
        "android.graphics.drawable.ColorDrawable",
        "android.os.Handler",
        "android.os.Looper",
        "android.text.Editable",
        "android.view.View",
        "android.view.Window",
        "android.view.ViewGroup.LayoutParams",
        "android.widget.EditText",
        "android.widget.FrameLayout",
        "android.widget.FrameLayout.LayoutParams",
        "android.widget.ImageView"
    };
    private static final String[] CATVOD_STUBS = new String[] {
        "com.github.catvod.crawler.Spider",
        "com.github.catvod.net.OkHttp",
        "okhttp3.* bridge subset"
    };
    private static final String[] UNSUPPORTED_API = new String[] {
        "WebView",
        "OCR",
        "deep android framework",
        "native app signature"
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
        String artifactPath = stringValue(request.get("artifactPath"));
        String workDir = defaultString(stringValue(request.get("workDir")), System.getProperty("java.io.tmpdir"));
        String extend = stringValue(request.get("extend"));
        @SuppressWarnings("unchecked")
        Map<String, Object> callArgs = request.get("args") instanceof Map ? (Map<String, Object>) request.get("args") : new HashMap<>();
        try {
            DexResult dex = convertDex(artifactPath, workDir);
            emitLog("dex2jar 转换完成: " + dex.outputPath);
            Object spider = createSpider(className, dex.outputPath, extend);
            Object data = callSpider(spider, method, callArgs);
            Map<String, Object> out = baseResult(true, method, className, start);
            out.put("data", data);
            out.put("dex2jar", dex.toMap(true, null, null, System.currentTimeMillis() - start));
            emitResult(out);
        } catch (Throwable t) {
            Throwable root = t.getCause() != null ? t.getCause() : t;
            emitResult(error(method, className, root.toString(), errorType(root), start));
        }
    }

    private static Object createSpider(String className, String classJarPath, String extend) throws Exception {
        if (className == null || className.trim().isEmpty()) {
            throw new IllegalArgumentException("className 为空");
        }
        ClassLoader parent = CSPProbe.class.getClassLoader();
        URL[] urls = new URL[] { Path.of(classJarPath).toUri().toURL() };
        URLClassLoader loader = new URLClassLoader(urls, parent);
        Application app = new Application();
        initCatVodHost(loader, app);
        Class<?> clazz = Class.forName(className, true, loader);
        Object instance = clazz.getDeclaredConstructor().newInstance();
        if (instance instanceof Spider) {
            ((Spider) instance).init(app, extend);
        } else {
            Method init = findMethod(clazz, "init", Context.class, String.class);
            if (init != null) {
                init.invoke(instance, app, extend);
            }
        }
        return instance;
    }

    private static void initCatVodHost(ClassLoader loader, Application app) {
        try {
            Class<?> initClass = Class.forName("com.github.catvod.spider.Init", true, loader);
            Object initInstance = null;
            Method get = findStaticMethod(initClass, "get");
            if (get != null) {
                initInstance = get.invoke(null);
            }
            injectApplicationFields(initClass, initInstance, app);
            injectApplicationFields(initClass, null, app);
        } catch (Throwable t) {
            Throwable root = t.getCause() != null ? t.getCause() : t;
            emitLog("CatVod Init 宿主字段注入跳过: " + root);
        }
    }

    private static void injectApplicationFields(Class<?> clazz, Object target, Application app) {
        for (Field field : clazz.getDeclaredFields()) {
            if (!Application.class.isAssignableFrom(field.getType())) {
                continue;
            }
            boolean isStatic = Modifier.isStatic(field.getModifiers());
            if ((target == null && !isStatic) || (target != null && isStatic)) {
                continue;
            }
            try {
                field.setAccessible(true);
                field.set(isStatic ? null : target, app);
            } catch (Throwable ignored) {
            }
        }
    }

    private static DexResult convertDex(String artifactPath, String workDir) throws Exception {
        if (artifactPath == null || artifactPath.trim().isEmpty()) {
            throw new IllegalArgumentException("artifactPath 为空");
        }
        long start = System.currentTimeMillis();
        Path artifact = Path.of(artifactPath);
        String artifactName = artifact.getFileName() == null ? "spider" : artifact.getFileName().toString();
        Path root = Path.of(workDir).toAbsolutePath().normalize();
        Files.createDirectories(root);
        Path dexPath = root.resolve(safeName(artifactName) + ".classes.dex");
        Path output = root.resolve(safeName(artifactName) + ".classes.jar");
        try (JarFile jar = new JarFile(artifact.toFile())) {
            JarEntry dex = jar.getJarEntry("classes.dex");
            if (dex == null) {
                Enumeration<JarEntry> entries = jar.entries();
                while (entries.hasMoreElements()) {
                    JarEntry entry = entries.nextElement();
                    if (!entry.isDirectory() && entry.getName().endsWith(".dex")) {
                        dex = entry;
                        break;
                    }
                }
            }
            if (dex == null) {
                throw new IllegalArgumentException("spider jar 未包含 classes.dex");
            }
            try (InputStream in = jar.getInputStream(dex)) {
                Files.copy(in, dexPath, StandardCopyOption.REPLACE_EXISTING);
            }
        }
        Dex2jar.from(dexPath.toFile()).skipDebug(true).reUseReg(true).to(output);
        DexResult result = new DexResult();
        result.inputPath = dexPath.toString();
        result.outputPath = output.toString();
        result.tool = "de.femtopedia.dex2jar:dex-translator";
        result.durationMs = System.currentTimeMillis() - start;
        return result;
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
            case "proxy":
                return invokeProxy(clazz, spider, args);
            default:
                throw new UnsupportedOperationException("暂不支持的 CSP runtime method: " + method);
        }
    }

    private static Object invokeProxy(Class<?> clazz, Object spider, Map<String, Object> args) throws Exception {
        Map<String, String> params = stringMap(args);
        Method method = findMethod(clazz, "proxy", Map.class);
        if (method != null) {
            return normalizeProxyResult(method.invoke(spider, params));
        }
        method = findMethod(clazz, "proxy", String.class);
        if (method != null) {
            return normalizeProxyResult(method.invoke(spider, stringArg(args, "id", "url", "")));
        }
        method = findStaticMethod(clazz, "proxy", Map.class);
        if (method != null) {
            return normalizeProxyResult(method.invoke(null, params));
        }
        ClassLoader loader = clazz.getClassLoader();
        Class<?> proxyClass = Class.forName("com.github.catvod.spider.Proxy", true, loader);
        method = findStaticMethod(proxyClass, "proxy", Map.class);
        if (method != null) {
            return normalizeProxyResult(method.invoke(null, params));
        }
        throw new UnsupportedOperationException("该 spider 未提供 proxy 宿主入口");
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

    private static Method findStaticMethod(Class<?> clazz, String name, Class<?>... types) {
        Method method = findMethod(clazz, name, types);
        if (method != null && java.lang.reflect.Modifier.isStatic(method.getModifiers())) {
            return method;
        }
        return null;
    }

    private static Map<String, String> stringMap(Map<String, Object> args) {
        Map<String, String> out = new HashMap<>();
        for (Map.Entry<String, Object> entry : args.entrySet()) {
            out.put(entry.getKey(), stringValue(entry.getValue()));
        }
        return out;
    }

    private static Object normalizeProxyResult(Object value) {
        if (value == null) {
            return "";
        }
        if (value instanceof Object[]) {
            List<Object> out = new ArrayList<>();
            for (Object item : (Object[]) value) {
                out.add(normalizeProxyResult(item));
            }
            return out;
        }
        if (value instanceof byte[]) {
            Map<String, Object> out = new HashMap<>();
            out.put("bodyBase64", java.util.Base64.getEncoder().encodeToString((byte[]) value));
            return out;
        }
        return value;
    }

    private static Map<String, Object> baseResult(boolean ok, String method, String className, long start) {
        Map<String, Object> out = new HashMap<>();
        out.put("ok", ok);
        out.put("method", method);
        out.put("className", className);
        out.put("durationMs", System.currentTimeMillis() - start);
        out.put("androidStubs", ANDROID_STUBS);
        out.put("catVodStubs", CATVOD_STUBS);
        out.put("networkBridge", "okhttp3-go-bridge");
        out.put("unsupportedApi", UNSUPPORTED_API);
        return out;
    }

    private static Map<String, Object> error(String method, String className, String message, String type, long start) {
        Map<String, Object> out = baseResult(false, method, className, start);
        out.put("error", message);
        out.put("errorType", type);
        Map<String, Object> dex = new HashMap<>();
        dex.put("ok", false);
        dex.put("tool", "de.femtopedia.dex2jar:dex-translator");
        dex.put("error", message);
        dex.put("errorType", type);
        dex.put("durationMs", System.currentTimeMillis() - start);
        out.put("dex2jar", dex);
        return out;
    }

    private static void emitLog(String message) {
        Map<String, Object> wrapper = new HashMap<>();
        wrapper.put("type", "log");
        wrapper.put("message", message);
        System.out.println(Json.stringify(wrapper));
        System.out.flush();
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
        if (out.isEmpty() && "flag".equals(singleKey)) {
            out.add("");
        }
        return out;
    }

    private static String defaultString(String value, String fallback) {
        return value == null || value.trim().isEmpty() ? fallback : value;
    }

    private static String stringValue(Object value) {
        return value == null ? "" : String.valueOf(value);
    }

    private static String safeName(String value) {
        return value.replaceAll("[^A-Za-z0-9._-]", "_");
    }

    private static final class DexResult {
        String tool;
        String inputPath;
        String outputPath;
        long durationMs;

        Map<String, Object> toMap(boolean ok, String error, String errorType, long fallbackDurationMs) {
            Map<String, Object> out = new HashMap<>();
            out.put("ok", ok);
            out.put("tool", tool);
            out.put("inputPath", inputPath);
            out.put("outputPath", outputPath);
            out.put("durationMs", durationMs > 0 ? durationMs : fallbackDurationMs);
            if (error != null && !error.isEmpty()) {
                out.put("error", error);
            }
            if (errorType != null && !errorType.isEmpty()) {
                out.put("errorType", errorType);
            }
            return out;
        }
    }
}
