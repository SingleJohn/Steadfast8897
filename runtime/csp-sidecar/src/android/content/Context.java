package android.content;

import android.content.pm.PackageManager;
import java.io.File;

public class Context {
    public static final int MODE_PRIVATE = 0;

    public SharedPreferences getSharedPreferences(String name, int mode) {
        return new MemorySharedPreferences();
    }

    public PackageManager getPackageManager() {
        return new PackageManager();
    }

    public String getPackageName() {
        return "fyms.csp.sidecar";
    }

    public File getFilesDir() {
        File dir = new File(System.getProperty("java.io.tmpdir"), "fyms-csp-files");
        dir.mkdirs();
        return dir;
    }
}
