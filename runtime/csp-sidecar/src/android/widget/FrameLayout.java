package android.widget;

import android.content.Context;
import android.view.View;
import android.view.ViewGroup;

public class FrameLayout extends ViewGroup {
    public FrameLayout(Context context) {}

    public void addView(View view) {}

    public void addView(View view, ViewGroup.LayoutParams params) {}

    public static class LayoutParams extends ViewGroup.LayoutParams {
        public int gravity;

        public LayoutParams(int width, int height) {
            super(width, height);
        }
    }
}
