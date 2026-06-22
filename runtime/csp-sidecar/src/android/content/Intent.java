package android.content;

import android.net.Uri;

public class Intent {
    private String action;
    private Uri data;
    private ComponentName component;

    public Intent() {}

    public Intent(String action) {
        this.action = action;
    }

    public Intent setData(Uri data) {
        this.data = data;
        return this;
    }

    public Intent setComponent(ComponentName component) {
        this.component = component;
        return this;
    }

    public String getAction() {
        return action;
    }

    public Uri getData() {
        return data;
    }

    public ComponentName getComponent() {
        return component;
    }
}
