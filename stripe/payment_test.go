package stripe

import "testing"

func TestDashboardURL_TestMode(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test_abc123"}
	got := sc.DashboardURL("pi_3ABC")
	want := "https://dashboard.stripe.com/test/payments/pi_3ABC"
	if got != want {
		t.Errorf("DashboardURL() = %q, want %q", got, want)
	}
}

func TestDashboardURL_LiveMode(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_live_abc123"}
	got := sc.DashboardURL("pi_3ABC")
	want := "https://dashboard.stripe.com/payments/pi_3ABC"
	if got != want {
		t.Errorf("DashboardURL() = %q, want %q", got, want)
	}
}

func TestDashboardURL_EmptyID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test_abc123"}
	got := sc.DashboardURL("")
	if got != "" {
		t.Errorf("DashboardURL(\"\") = %q, want empty string", got)
	}
}
