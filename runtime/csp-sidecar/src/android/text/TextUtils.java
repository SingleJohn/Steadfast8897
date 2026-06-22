package android.text;

import java.util.Iterator;

public final class TextUtils {
    private TextUtils() {}

    public static boolean isEmpty(CharSequence text) {
        return text == null || text.length() == 0;
    }

    public static boolean equals(CharSequence a, CharSequence b) {
        if (a == null && b == null) {
            return true;
        }
        if (a == null || b == null) {
            return false;
        }
        return a.toString().contentEquals(b);
    }

    public static String join(CharSequence delimiter, Iterable<?> tokens) {
        StringBuilder sb = new StringBuilder();
        Iterator<?> it = tokens.iterator();
        while (it.hasNext()) {
            if (sb.length() > 0) {
                sb.append(delimiter);
            }
            sb.append(it.next());
        }
        return sb.toString();
    }

    public static String join(CharSequence delimiter, Object[] tokens) {
        StringBuilder sb = new StringBuilder();
        for (Object token : tokens) {
            if (sb.length() > 0) {
                sb.append(delimiter);
            }
            sb.append(token);
        }
        return sb.toString();
    }
}
