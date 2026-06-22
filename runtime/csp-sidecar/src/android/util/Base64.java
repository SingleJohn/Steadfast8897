package android.util;

public final class Base64 {
    public static final int DEFAULT = 0;
    public static final int NO_WRAP = 2;

    private Base64() {}

    public static byte[] decode(String value, int flags) {
        return java.util.Base64.getDecoder().decode(value);
    }

    public static String encodeToString(byte[] value, int flags) {
        java.util.Base64.Encoder encoder = (flags & NO_WRAP) != 0
            ? java.util.Base64.getEncoder().withoutPadding()
            : java.util.Base64.getEncoder();
        return encoder.encodeToString(value);
    }
}
