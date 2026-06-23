package fyms.csp;

import java.io.BufferedReader;
import java.io.FileDescriptor;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;

public final class RpcIO {
    private static final BufferedReader IN = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
    private static final PrintStream OUT = new PrintStream(new FileOutputStream(FileDescriptor.out), true, StandardCharsets.UTF_8);

    private RpcIO() {}

    public static synchronized String readLine() throws java.io.IOException {
        return IN.readLine();
    }

    public static synchronized void writeJsonLine(Object value) {
        OUT.println(Json.stringify(value));
        OUT.flush();
    }
}
