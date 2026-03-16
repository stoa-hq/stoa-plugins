package stripe

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	stripelib "github.com/stripe/stripe-go/v82"
	stripeclient "github.com/stripe/stripe-go/v82/client"
)

// testRedirectTransport leitet alle HTTP-Anfragen auf den angegebenen Test-Server um.
type testRedirectTransport struct {
	targetHost string
}

func (t *testRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	targetURL, _ := url.Parse("http://" + t.targetHost)
	clone.URL.Scheme = targetURL.Scheme
	clone.URL.Host = targetURL.Host
	clone.Host = targetURL.Host
	return http.DefaultTransport.RoundTrip(clone)
}

// newTestStripeClientFromServer erstellt einen stripeClient, der Requests an den
// übergebenen httptest.Server sendet. Der Aufrufer ist für das Schließen des Servers
// verantwortlich.
func newTestStripeClientFromServer(t *testing.T, ts *httptest.Server) *stripeClient {
	t.Helper()

	targetHost := ts.Listener.Addr().String()
	httpClient := &http.Client{Transport: &testRedirectTransport{targetHost: targetHost}}
	backends := stripelib.NewBackends(httpClient)

	api := &stripeclient.API{}
	api.Init("sk_test_fake", backends)

	return &stripeClient{
		api:            api,
		publishableKey: "pk_test_fake",
		currency:       "EUR",
	}
}

// newTestStripeClient erstellt einen stripeClient mit einem internen httptest.Server,
// der immer den angegebenen statusCode und body zurückgibt. Der Server wird via
// t.Cleanup automatisch geschlossen.
func newTestStripeClient(t *testing.T, statusCode int, body string) (*httptest.Server, *stripeClient) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)
	return ts, sc
}
