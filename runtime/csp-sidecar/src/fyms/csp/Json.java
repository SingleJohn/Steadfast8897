package fyms.csp;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class Json {
    private Json() {}

    public static Map<String, Object> parseObject(String raw) {
        Object value = new Parser(raw).parseValue();
        if (value instanceof Map) {
            @SuppressWarnings("unchecked")
            Map<String, Object> out = (Map<String, Object>) value;
            return out;
        }
        return new LinkedHashMap<>();
    }

    public static String stringify(Object value) {
        StringBuilder sb = new StringBuilder();
        writeValue(sb, value);
        return sb.toString();
    }

    private static void writeValue(StringBuilder sb, Object value) {
        if (value == null) {
            sb.append("null");
        } else if (value instanceof Number || value instanceof Boolean) {
            sb.append(value);
        } else if (value instanceof Map) {
            sb.append('{');
            boolean first = true;
            for (Object entryObj : ((Map<?, ?>) value).entrySet()) {
                Map.Entry<?, ?> entry = (Map.Entry<?, ?>) entryObj;
                if (!first) {
                    sb.append(',');
                }
                first = false;
                writeString(sb, String.valueOf(entry.getKey()));
                sb.append(':');
                writeValue(sb, entry.getValue());
            }
            sb.append('}');
        } else if (value instanceof Iterable) {
            sb.append('[');
            boolean first = true;
            for (Object item : (Iterable<?>) value) {
                if (!first) {
                    sb.append(',');
                }
                first = false;
                writeValue(sb, item);
            }
            sb.append(']');
        } else if (value.getClass().isArray()) {
            sb.append('[');
            int len = java.lang.reflect.Array.getLength(value);
            for (int i = 0; i < len; i++) {
                if (i > 0) {
                    sb.append(',');
                }
                writeValue(sb, java.lang.reflect.Array.get(value, i));
            }
            sb.append(']');
        } else {
            writeString(sb, String.valueOf(value));
        }
    }

    private static void writeString(StringBuilder sb, String value) {
        sb.append('"');
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            switch (c) {
                case '"': sb.append("\\\""); break;
                case '\\': sb.append("\\\\"); break;
                case '\b': sb.append("\\b"); break;
                case '\f': sb.append("\\f"); break;
                case '\n': sb.append("\\n"); break;
                case '\r': sb.append("\\r"); break;
                case '\t': sb.append("\\t"); break;
                default:
                    if (c < 0x20) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
            }
        }
        sb.append('"');
    }

    private static final class Parser {
        private final String raw;
        private int pos;

        Parser(String raw) {
            this.raw = raw == null ? "" : raw;
        }

        Object parseValue() {
            skipSpace();
            if (pos >= raw.length()) {
                return null;
            }
            char c = raw.charAt(pos);
            if (c == '"') {
                return parseString();
            }
            if (c == '{') {
                return parseObject();
            }
            if (c == '[') {
                return parseArray();
            }
            if (raw.startsWith("true", pos)) {
                pos += 4;
                return Boolean.TRUE;
            }
            if (raw.startsWith("false", pos)) {
                pos += 5;
                return Boolean.FALSE;
            }
            if (raw.startsWith("null", pos)) {
                pos += 4;
                return null;
            }
            return parseNumber();
        }

        private Map<String, Object> parseObject() {
            Map<String, Object> out = new LinkedHashMap<>();
            pos++;
            skipSpace();
            while (pos < raw.length() && raw.charAt(pos) != '}') {
                String key = parseString();
                skipSpace();
                if (pos < raw.length() && raw.charAt(pos) == ':') {
                    pos++;
                }
                Object value = parseValue();
                out.put(key, value);
                skipSpace();
                if (pos < raw.length() && raw.charAt(pos) == ',') {
                    pos++;
                    skipSpace();
                }
            }
            if (pos < raw.length() && raw.charAt(pos) == '}') {
                pos++;
            }
            return out;
        }

        private List<Object> parseArray() {
            List<Object> out = new ArrayList<>();
            pos++;
            skipSpace();
            while (pos < raw.length() && raw.charAt(pos) != ']') {
                out.add(parseValue());
                skipSpace();
                if (pos < raw.length() && raw.charAt(pos) == ',') {
                    pos++;
                    skipSpace();
                }
            }
            if (pos < raw.length() && raw.charAt(pos) == ']') {
                pos++;
            }
            return out;
        }

        private String parseString() {
            StringBuilder sb = new StringBuilder();
            if (pos < raw.length() && raw.charAt(pos) == '"') {
                pos++;
            }
            while (pos < raw.length()) {
                char c = raw.charAt(pos++);
                if (c == '"') {
                    break;
                }
                if (c == '\\' && pos < raw.length()) {
                    char e = raw.charAt(pos++);
                    switch (e) {
                        case '"': sb.append('"'); break;
                        case '\\': sb.append('\\'); break;
                        case '/': sb.append('/'); break;
                        case 'b': sb.append('\b'); break;
                        case 'f': sb.append('\f'); break;
                        case 'n': sb.append('\n'); break;
                        case 'r': sb.append('\r'); break;
                        case 't': sb.append('\t'); break;
                        case 'u':
                            if (pos + 4 <= raw.length()) {
                                sb.append((char) Integer.parseInt(raw.substring(pos, pos + 4), 16));
                                pos += 4;
                            }
                            break;
                        default:
                            sb.append(e);
                    }
                } else {
                    sb.append(c);
                }
            }
            return sb.toString();
        }

        private Number parseNumber() {
            int start = pos;
            while (pos < raw.length()) {
                char c = raw.charAt(pos);
                if ((c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E') {
                    pos++;
                } else {
                    break;
                }
            }
            String text = raw.substring(start, pos);
            if (text.contains(".") || text.contains("e") || text.contains("E")) {
                return Double.parseDouble(text);
            }
            try {
                return Long.parseLong(text);
            } catch (NumberFormatException e) {
                return 0;
            }
        }

        private void skipSpace() {
            while (pos < raw.length() && Character.isWhitespace(raw.charAt(pos))) {
                pos++;
            }
        }
    }
}
