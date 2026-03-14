package stripe

import (
	"strings"
	"testing"

	"github.com/stoa-hq/stoa/pkg/sdk"
)

func TestPlugin_Metadata(t *testing.T) {
	p := New()

	if p.Name() != "stripe" {
		t.Errorf("Name() = %q, want %q", p.Name(), "stripe")
	}
	if p.Version() == "" {
		t.Error("Version() must not be empty")
	}
	if p.Description() == "" {
		t.Error("Description() must not be empty")
	}
}

func TestPlugin_Shutdown(t *testing.T) {
	p := New()
	if err := p.Shutdown(); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestPlugin_UIExtensions(t *testing.T) {
	p := New()

	// Plugin must implement sdk.UIPlugin.
	var _ sdk.UIPlugin = p

	exts := p.UIExtensions()
	if len(exts) == 0 {
		t.Fatal("UIExtensions() returned no extensions")
	}

	ext := exts[0]
	if ext.ID != "stripe_checkout" {
		t.Errorf("extension ID = %q, want %q", ext.ID, "stripe_checkout")
	}
	if ext.Slot != "storefront:checkout:payment" {
		t.Errorf("extension Slot = %q", ext.Slot)
	}
	if ext.Type != "component" {
		t.Errorf("extension Type = %q, want %q", ext.Type, "component")
	}
	if ext.Component == nil {
		t.Fatal("extension Component is nil")
	}
	if ext.Component.TagName != "stoa-stripe-checkout" {
		t.Errorf("TagName = %q", ext.Component.TagName)
	}
	if !strings.HasPrefix(ext.Component.TagName, "stoa-stripe-") {
		t.Error("TagName must start with stoa-stripe-")
	}
	if ext.Component.ScriptURL == "" {
		t.Error("ScriptURL must not be empty")
	}
	if ext.Component.Integrity == "" {
		t.Error("Integrity (SRI hash) must not be empty")
	}
	if !strings.HasPrefix(ext.Component.Integrity, "sha256-") {
		t.Errorf("Integrity = %q, want sha256- prefix", ext.Component.Integrity)
	}
	if len(ext.Component.ExternalScripts) == 0 {
		t.Error("ExternalScripts should include Stripe.js URL")
	}

	// Validate extension against SDK rules.
	if err := sdk.ValidateUIExtension("stripe", ext); err != nil {
		t.Errorf("ValidateUIExtension() error = %v", err)
	}
}

func TestPlugin_SRIHash(t *testing.T) {
	hash := sriHash("frontend/dist/checkout.js")
	if hash == "" {
		t.Fatal("sriHash returned empty for embedded checkout.js")
	}
	if !strings.HasPrefix(hash, "sha256-") {
		t.Errorf("sriHash = %q, want sha256- prefix", hash)
	}
}

func TestPlugin_AssetsSubFS(t *testing.T) {
	fsys := assetsSubFS()
	f, err := fsys.Open("checkout.js")
	if err != nil {
		t.Fatalf("assetsSubFS: open checkout.js: %v", err)
	}
	f.Close()
}

func TestPlugin_Init_MissingConfig(t *testing.T) {
	// We can't use sdk.AppContext directly in isolation, so we test
	// configFrom which is the first thing Init calls.
	if _, err := configFrom(map[string]interface{}{}); err == nil {
		t.Error("expected error when stripe config section is absent")
	}
}
