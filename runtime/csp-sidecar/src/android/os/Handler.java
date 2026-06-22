package android.os;

public class Handler {
    public Handler() {}

    public Handler(Looper looper) {}

    public boolean post(Runnable action) {
        if (action != null) {
            action.run();
        }
        return true;
    }

    public boolean postDelayed(Runnable action, long delayMillis) {
        return post(action);
    }
}
