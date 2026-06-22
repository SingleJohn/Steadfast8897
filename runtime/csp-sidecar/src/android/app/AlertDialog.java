package android.app;

import android.content.Context;

public class AlertDialog {
    public void show() {}
    public void dismiss() {}

    public static class Builder {
        public Builder(Context context) {}

        public Builder setTitle(CharSequence title) {
            return this;
        }

        public Builder setMessage(CharSequence message) {
            return this;
        }

        public AlertDialog create() {
            return new AlertDialog();
        }

        public AlertDialog show() {
            return new AlertDialog();
        }
    }
}
