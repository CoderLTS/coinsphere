package security

import (
	"errors"
	"testing"
	"time"
)

func TestAccessTokenRequiresSessionVersion(t *testing.T) {
	manager := NewTokenManager("synthetic-key", 60)
	for _, version := range []any{nil, int64(0), int64(-1)} {
		payload := map[string]any{"sub": 1, "typ": "access", "exp": time.Now().Add(time.Hour).Unix(), "jti": "synthetic-session"}
		if version != nil {
			payload["sv"] = version
		}
		input := b64JSON(map[string]any{"alg": "HS256", "typ": "JWT"}) + "." + b64JSON(payload)
		if _, err := manager.VerifyAccessToken(input + "." + manager.sign(input)); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("legacy or invalid session version accepted", version, err)
		}
	}
	token := manager.CreateAccessToken(1, 2, false)
	payload, err := manager.VerifyAccessToken(token.Value)
	if err != nil || payload.SessionVersion != 2 {
		t.Fatal("valid session rejected", err)
	}
}
