package android.widget;

import android.content.Context;
import android.view.View;

public class ImageView extends View {
    public enum ScaleType {
        CENTER,
        CENTER_CROP,
        CENTER_INSIDE,
        FIT_CENTER
    }

    public ImageView(Context context) {}

    public void setImageBitmap(Object bitmap) {}

    public void setScaleType(ScaleType scaleType) {}
}
