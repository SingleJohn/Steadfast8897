package okhttp3;

import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.Arrays;
import java.util.List;

public interface Dns {
    Dns SYSTEM = hostname -> Arrays.asList(InetAddress.getAllByName(hostname));

    List<InetAddress> lookup(String hostname) throws UnknownHostException;
}
