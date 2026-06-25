package okhttp3;

import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import javax.net.ssl.HostnameVerifier;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.X509TrustManager;

public class OkHttpClient {
    private final Dispatcher dispatcher;
    private final boolean followRedirects;
    private final boolean followSslRedirects;
    private final boolean retryOnConnectionFailure;

    public OkHttpClient() {
        this(new Builder());
    }

    private OkHttpClient(Builder builder) {
        this.dispatcher = builder.dispatcher;
        this.followRedirects = builder.followRedirects;
        this.followSslRedirects = builder.followSslRedirects;
        this.retryOnConnectionFailure = builder.retryOnConnectionFailure;
    }

    public Call newCall(Request request) {
        BridgeCall call = new BridgeCall(request, dispatcher);
        dispatcher.addRunning(call);
        return call;
    }

    public Builder newBuilder() {
        return new Builder()
            .dispatcher(dispatcher)
            .followRedirects(followRedirects)
            .followSslRedirects(followSslRedirects)
            .retryOnConnectionFailure(retryOnConnectionFailure);
    }

    public Dispatcher dispatcher() {
        return dispatcher;
    }

    public boolean retryOnConnectionFailure() {
        return retryOnConnectionFailure;
    }

    public static class Builder {
        private Dispatcher dispatcher = new Dispatcher();
        private boolean followRedirects = true;
        private boolean followSslRedirects = true;
        private boolean retryOnConnectionFailure = true;

        public Builder dns(Dns dns) {
            return this;
        }

        public Builder readTimeout(long timeout, TimeUnit unit) {
            return this;
        }

        public Builder writeTimeout(long timeout, TimeUnit unit) {
            return this;
        }

        public Builder connectTimeout(long timeout, TimeUnit unit) {
            return this;
        }

        public Builder hostnameVerifier(HostnameVerifier verifier) {
            return this;
        }

        public Builder sslSocketFactory(SSLSocketFactory factory, X509TrustManager trustManager) {
            return this;
        }

        public Builder followRedirects(boolean followRedirects) {
            this.followRedirects = followRedirects;
            return this;
        }

        public Builder followSslRedirects(boolean followSslRedirects) {
            this.followSslRedirects = followSslRedirects;
            return this;
        }

        public Builder retryOnConnectionFailure(boolean retryOnConnectionFailure) {
            this.retryOnConnectionFailure = retryOnConnectionFailure;
            return this;
        }

        public Builder dispatcher(Dispatcher dispatcher) {
            if (dispatcher != null) {
                this.dispatcher = dispatcher;
            }
            return this;
        }

        public OkHttpClient build() {
            return new OkHttpClient(this);
        }
    }

    private static final class BridgeCall implements Call {
        private final Request request;
        private final Dispatcher dispatcher;
        private volatile boolean canceled;

        BridgeCall(Request request, Dispatcher dispatcher) {
            this.request = request;
            this.dispatcher = dispatcher;
        }

        @Override
        public Response execute() throws IOException {
            if (canceled) {
                throw new IOException("call canceled");
            }
            try {
                Response response = fyms.csp.HttpBridge.execute(request);
                dispatcher.removeRunning(this);
                return response;
            } catch (RuntimeException e) {
                dispatcher.removeRunning(this);
                throw e;
            }
        }

        @Override
        public Request request() {
            return request;
        }

        @Override
        public void cancel() {
            canceled = true;
            dispatcher.removeRunning(this);
        }
    }
}
