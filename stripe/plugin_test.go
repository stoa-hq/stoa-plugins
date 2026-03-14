package stripe

import "testing"

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

func TestPlugin_Init_MissingConfig(t *testing.T) {
	// We can't use sdk.AppContext directly in isolation, so we test
	// configFrom which is the first thing Init calls.
	if _, err := configFrom(map[string]interface{}{}); err == nil {
		t.Error("expected error when stripe config section is absent")
	}
}
