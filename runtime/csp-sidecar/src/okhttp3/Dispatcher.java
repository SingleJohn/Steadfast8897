package okhttp3;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public class Dispatcher {
    private final List<Call> running = Collections.synchronizedList(new ArrayList<>());
    private final List<Call> queued = Collections.synchronizedList(new ArrayList<>());

    void addRunning(Call call) {
        running.add(call);
    }

    void removeRunning(Call call) {
        running.remove(call);
    }

    public List<Call> queuedCalls() {
        return new ArrayList<>(queued);
    }

    public List<Call> runningCalls() {
        return new ArrayList<>(running);
    }
}
