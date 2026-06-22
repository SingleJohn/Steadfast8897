package android.util;

public final class Log {
    private Log() {}

    public static int d(String tag, String message) {
        System.err.println("[D][" + tag + "] " + message);
        return 0;
    }

    public static int i(String tag, String message) {
        System.err.println("[I][" + tag + "] " + message);
        return 0;
    }

    public static int w(String tag, String message) {
        System.err.println("[W][" + tag + "] " + message);
        return 0;
    }

    public static int e(String tag, String message) {
        System.err.println("[E][" + tag + "] " + message);
        return 0;
    }
}
