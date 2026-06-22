package android.app;

import android.content.Context;

public class Activity extends Context {
    public void runOnUiThread(Runnable action) {
        if (action != null) {
            action.run();
        }
    }
}
