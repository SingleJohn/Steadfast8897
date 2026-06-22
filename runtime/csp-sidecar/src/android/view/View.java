package android.view;

public class View {
    public static final int VISIBLE = 0;
    public static final int INVISIBLE = 4;
    public static final int GONE = 8;

    private int visibility = VISIBLE;

    public void setVisibility(int visibility) {
        this.visibility = visibility;
    }

    public int getVisibility() {
        return visibility;
    }
}
