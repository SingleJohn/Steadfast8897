package android.content.pm;

public class PackageManager {
    public static final int GET_ACTIVITIES = 1;

    public PackageInfo getPackageInfo(String packageName, int flags) throws NameNotFoundException {
        throw new NameNotFoundException(packageName);
    }

    public ApplicationInfo getApplicationInfo(String packageName, int flags) throws NameNotFoundException {
        throw new NameNotFoundException(packageName);
    }

    public static class NameNotFoundException extends Exception {
        public NameNotFoundException() {
            super();
        }

        public NameNotFoundException(String name) {
            super(name);
        }
    }
}
