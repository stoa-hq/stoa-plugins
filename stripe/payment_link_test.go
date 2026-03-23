package stripe

import (
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error: %v", err)
	}
	if token == "" {
		t.Fatal("generateToken() returned empty string")
	}
	// 32 bytes base64url-encoded without padding = 43 chars
	if len(token) != 43 {
		t.Errorf("token length = %d, want 43", len(token))
	}

	// Tokens must be unique
	token2, err := generateToken()
	if err != nil {
		t.Fatalf("second generateToken() error: %v", err)
	}
	if token == token2 {
		t.Error("two consecutive tokens are identical")
	}
}

func TestGenerateToken_URLSafe(t *testing.T) {
	for i := 0; i < 50; i++ {
		token, err := generateToken()
		if err != nil {
			t.Fatalf("generateToken() error: %v", err)
		}
		for _, c := range token {
			if c == '+' || c == '/' || c == '=' {
				t.Errorf("token contains non-URL-safe character %q: %s", c, token)
				break
			}
		}
	}
}
