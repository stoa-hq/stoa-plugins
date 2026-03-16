package stripe

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

func TestValidateCheckoutPayment_NonStripeProvider(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "manual",
			"payment_reference": "",
		},
	}
	if err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop()); err != nil {
		t.Errorf("non-stripe provider should be skipped, got error: %v", err)
	}
}

func TestValidateCheckoutPayment_EmptyProvider(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "",
			"payment_reference": "",
		},
	}
	if err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop()); err != nil {
		t.Errorf("empty provider should be skipped, got error: %v", err)
	}
}

func TestValidateCheckoutPayment_StripeProvider_EmptyReference(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "",
		},
	}
	err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop())
	if err == nil {
		t.Error("expected error for empty payment_reference with stripe provider")
	}
}

func TestValidateCheckoutPayment_NilMetadata(t *testing.T) {
	event := &sdk.HookEvent{}
	// No metadata at all — provider will be "" → skip
	if err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop()); err != nil {
		t.Errorf("nil metadata should be skipped, got error: %v", err)
	}
}
