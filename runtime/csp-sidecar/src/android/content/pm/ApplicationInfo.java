package android.content.pm;

public class ApplicationInfo {
    public String packageName = "";
    public CharSequence loadLabel(PackageManager pm) {
        return packageName;
    }
}
