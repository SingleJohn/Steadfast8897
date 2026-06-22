package okhttp3;

public class MediaType {
    private final String value;

    private MediaType(String value) {
        this.value = value;
    }

    public static MediaType parse(String value) {
        return new MediaType(value);
    }

    @Override
    public String toString() {
        return value;
    }
}
