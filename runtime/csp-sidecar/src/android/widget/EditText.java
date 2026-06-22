package android.widget;

import android.content.Context;

public class EditText {
    private CharSequence text = "";

    public EditText(Context context) {}

    public void setText(CharSequence text) {
        this.text = text == null ? "" : text;
    }

    public CharSequence getText() {
        return text;
    }
}
