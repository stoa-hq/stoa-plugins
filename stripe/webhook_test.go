package stripe

import (
	"encoding/json"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"
)

func TestExtractMetadata_Valid(t *testing.T) {
	pi := &stripe.PaymentIntent{
		ID: "pi_test",
		Metadata: map[string]string{
			"stoa_order_id":          "550e8400-e29b-41d4-a716-446655440000",
			"stoa_payment_method_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		},
	}

	orderID, pmID, err := extractMetadata(pi)
	if err != nil {
		t.Fatalf("extractMetadata() error = %v", err)
	}
	if orderID.String() != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("orderID = %s", orderID)
	}
	if pmID.String() != "6ba7b810-9dad-11d1-80b4-00c04fd430c8" {
		t.Errorf("paymentMethodID = %s", pmID)
	}
}

func TestExtractMetadata_MissingOrderID(t *testing.T) {
	pi := &stripe.PaymentIntent{
		Metadata: map[string]string{
			"stoa_payment_method_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		},
	}
	if _, _, err := extractMetadata(pi); err == nil {
		t.Error("expected error for missing stoa_order_id")
	}
}

func TestExtractMetadata_InvalidOrderID(t *testing.T) {
	pi := &stripe.PaymentIntent{
		Metadata: map[string]string{
			"stoa_order_id":          "not-a-uuid",
			"stoa_payment_method_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		},
	}
	if _, _, err := extractMetadata(pi); err == nil {
		t.Error("expected error for invalid stoa_order_id")
	}
}

func TestUnmarshalPaymentIntent_Valid(t *testing.T) {
	data := map[string]interface{}{
		"id":            "pi_test_123",
		"amount":        1999,
		"currency":      "eur",
		"client_secret": "pi_test_secret",
		"metadata": map[string]string{
			"stoa_order_id": "550e8400-e29b-41d4-a716-446655440000",
		},
	}
	raw, _ := json.Marshal(data)

	pi, err := unmarshalPaymentIntent(raw)
	if err != nil {
		t.Fatalf("unmarshalPaymentIntent() error = %v", err)
	}
	if pi.ID != "pi_test_123" {
		t.Errorf("ID = %q, want %q", pi.ID, "pi_test_123")
	}
	if pi.Amount != 1999 {
		t.Errorf("Amount = %d, want 1999", pi.Amount)
	}
}

func TestUnmarshalPaymentIntent_InvalidJSON(t *testing.T) {
	if _, err := unmarshalPaymentIntent(json.RawMessage(`{invalid}`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
