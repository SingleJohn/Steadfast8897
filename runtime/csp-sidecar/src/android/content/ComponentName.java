package android.content;

public class ComponentName {
    private final String pkg;
    private final String cls;

    public ComponentName(String pkg, String cls) {
        this.pkg = pkg == null ? "" : pkg;
        this.cls = cls == null ? "" : cls;
    }

    public String getPackageName() {
        return pkg;
    }

    public String getClassName() {
        return cls;
    }

    public String flattenToString() {
        return pkg + "/" + cls;
    }

    public String flattenToShortString() {
        return flattenToString();
    }

    @Override
    public String toString() {
        return flattenToString();
    }
}
