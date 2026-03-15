package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestDefaultSignatureConfig(t *testing.T) {
	cfg := DefaultSignatureConfig()

	if cfg.Enabled != false {
		t.Error("default Enabled should be false")
	}
	if cfg.HeaderName != "X-Signature" {
		t.Errorf("default HeaderName should be X-Signature, got %s", cfg.HeaderName)
	}
	if cfg.TimestampHeader != "X-Timestamp" {
		t.Errorf("default TimestampHeader should be X-Timestamp, got %s", cfg.TimestampHeader)
	}
	if cfg.NonceHeader != "X-Nonce" {
		t.Errorf("default NonceHeader should be X-Nonce, got %s", cfg.NonceHeader)
	}
	if cfg.KeyIDHeader != "X-Key-ID" {
		t.Errorf("default KeyIDHeader should be X-Key-ID, got %s", cfg.KeyIDHeader)
	}
	if cfg.MaxTimestampSkew != 5*time.Minute {
		t.Errorf("default MaxTimestampSkew should be 5 minutes, got %v", cfg.MaxTimestampSkew)
	}
	if cfg.NonceCacheSize != 10000 {
		t.Errorf("default NonceCacheSize should be 10000, got %d", cfg.NonceCacheSize)
	}
	if len(cfg.Algorithms) != 1 || cfg.Algorithms[0] != "SHA256" {
		t.Errorf("default Algorithms should be [SHA256], got %v", cfg.Algorithms)
	}
}

func TestNewSignatureVerifier(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		cfg := DefaultSignatureConfig()
		sv := NewSignatureVerifier(cfg)

		if sv.IsEnabled() != false {
			t.Error("should be disabled by default")
		}
	})

	t.Run("with empty config uses defaults", func(t *testing.T) {
		cfg := SignatureConfig{}
		sv := NewSignatureVerifier(cfg)

		if sv.config.HeaderName != "X-Signature" {
			t.Errorf("expected default HeaderName, got %s", sv.config.HeaderName)
		}
		if sv.config.MaxTimestampSkew != 5*time.Minute {
			t.Errorf("expected default MaxTimestampSkew, got %v", sv.config.MaxTimestampSkew)
		}
		if sv.config.NonceCacheSize != 10000 {
			t.Errorf("expected default NonceCacheSize, got %d", sv.config.NonceCacheSize)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		cfg := SignatureConfig{
			Enabled:          true,
			HeaderName:       "X-Custom-Sig",
			TimestampHeader:  "X-Custom-Ts",
			NonceHeader:      "X-Custom-Nonce",
			KeyIDHeader:      "X-Custom-Key",
			MaxTimestampSkew: 10 * time.Minute,
			NonceCacheSize:   5000,
			Algorithms:       []string{"SHA256", "SHA512"},
		}
		sv := NewSignatureVerifier(cfg)

		if sv.config.HeaderName != "X-Custom-Sig" {
			t.Errorf("expected X-Custom-Sig, got %s", sv.config.HeaderName)
		}
		if sv.config.MaxTimestampSkew != 10*time.Minute {
			t.Errorf("expected 10 minutes, got %v", sv.config.MaxTimestampSkew)
		}
	})
}

func TestAddRemoveSecrets(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())

	// Add secret
	sv.AddSecret("key1", "secret1")
	sv.mu.RLock()
	if len(sv.secrets) != 1 || sv.secrets["key1"] != "secret1" {
		t.Error("failed to add secret")
	}
	sv.mu.RUnlock()

	// Add another secret
	sv.AddSecret("key2", "secret2")
	sv.mu.RLock()
	if len(sv.secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(sv.secrets))
	}
	sv.mu.RUnlock()

	// Remove secret
	sv.RemoveSecret("key1")
	sv.mu.RLock()
	if _, exists := sv.secrets["key1"]; exists {
		t.Error("failed to remove secret")
	}
	if len(sv.secrets) != 1 {
		t.Errorf("expected 1 secret, got %d", len(sv.secrets))
	}
	sv.mu.RUnlock()

	// Remove non-existent secret (should not panic)
	sv.RemoveSecret("nonexistent")
}

func TestReloadSecrets(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())

	// Add initial secrets
	sv.AddSecret("key1", "secret1")
	sv.AddSecret("key2", "secret2")

	// Reload with new secrets
	newSecrets := map[string]string{
		"key3": "secret3",
		"key4": "secret4",
	}
	sv.ReloadSecrets(newSecrets)

	sv.mu.RLock()
	if len(sv.secrets) != 2 {
		t.Errorf("expected 2 secrets after reload, got %d", len(sv.secrets))
	}
	if sv.secrets["key3"] != "secret3" {
		t.Error("key3 not found or incorrect value")
	}
	if sv.secrets["key4"] != "secret4" {
		t.Error("key4 not found or incorrect value")
	}
	if _, exists := sv.secrets["key1"]; exists {
		t.Error("old key1 should not exist after reload")
	}
	sv.mu.RUnlock()
}

func TestSign(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())
	sv.AddSecret("test-key", "test-secret")

	timestamp := time.Now().Unix()
	nonce := "test-nonce-123"
	method := "GET"
	path := "/api/v1/test"
	body := ""

	sig := sv.SignWithKeyID(method, path, body, timestamp, nonce, "test-key")
	if sig == "" {
		t.Error("signature should not be empty")
	}

	// Verify signature format (hex string)
	if len(sig) != 64 { // SHA256 produces 32 bytes = 64 hex chars
		t.Errorf("expected 64 character hex signature, got %d", len(sig))
	}
}

func TestSignWithBody(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())
	sv.AddSecret("test-key", "test-secret")

	timestamp := time.Now().Unix()
	nonce := "test-nonce-456"
	method := "POST"
	path := "/api/v1/data"
	body := `{"name":"test","value":123}`

	sig := sv.SignWithKeyID(method, path, body, timestamp, nonce, "test-key")
	if sig == "" {
		t.Error("signature should not be empty")
	}

	// Verify the body hash is included
	bodyHash := sv.computeBodyHash(body)
	if bodyHash == "" {
		t.Error("body hash should not be empty for non-empty body")
	}
}

func TestComputeBodyHash(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())

	t.Run("empty body", func(t *testing.T) {
		hash := sv.computeBodyHash("")
		if hash != "" {
			t.Errorf("empty body should produce empty hash, got %s", hash)
		}
	})

	t.Run("non-empty body", func(t *testing.T) {
		body := "test body content"
		hash := sv.computeBodyHash(body)
		if hash == "" {
			t.Error("non-empty body should produce non-empty hash")
		}

		// Verify hash is consistent
		hash2 := sv.computeBodyHash(body)
		if hash != hash2 {
			t.Error("same body should produce same hash")
		}

		// Verify hash is different for different body
		hash3 := sv.computeBodyHash("different body")
		if hash == hash3 {
			t.Error("different bodies should produce different hashes")
		}
	})
}

func TestComputeHMAC(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())

	t.Run("SHA256", func(t *testing.T) {
		data := "test-data"
		secret := "test-secret"
		sig := sv.computeHMAC(data, secret, "SHA256")

		// Verify with standard library
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(data))
		expected := hex.EncodeToString(h.Sum(nil))

		if sig != expected {
			t.Errorf("SHA256 HMAC mismatch: got %s, expected %s", sig, expected)
		}
	})

	t.Run("SHA512", func(t *testing.T) {
		data := "test-data"
		secret := "test-secret"
		sig := sv.computeHMAC(data, secret, "SHA512")

		// Verify with standard library
		h := hmac.New(sha512.New, []byte(secret))
		h.Write([]byte(data))
		expected := hex.EncodeToString(h.Sum(nil))

		if sig != expected {
			t.Errorf("SHA512 HMAC mismatch: got %s, expected %s", sig, expected)
		}
	})

	t.Run("unknown algorithm defaults to SHA256", func(t *testing.T) {
		data := "test-data"
		secret := "test-secret"
		sig := sv.computeHMAC(data, secret, "UNKNOWN")

		// Should default to SHA256
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(data))
		expected := hex.EncodeToString(h.Sum(nil))

		if sig != expected {
			t.Error("unknown algorithm should default to SHA256")
		}
	})
}

func TestVerify(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	t.Run("valid signature", func(t *testing.T) {
		timestamp := time.Now().Unix()
		nonce := "nonce-valid-1"
		method := "GET"
		path := "/api/v1/test"
		body := ""

		// Generate signature
		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA256")

		// Create request
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !valid {
			t.Error("signature should be valid")
		}
	})

	t.Run("valid signature with body", func(t *testing.T) {
		timestamp := time.Now().Unix()
		nonce := "nonce-valid-2"
		method := "POST"
		path := "/api/v1/data"
		body := `{"name":"test"}`

		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA256")

		req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !valid {
			t.Error("signature should be valid")
		}
	})

	t.Run("missing signature header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Timestamp", "12345")
		req.Header.Set("X-Nonce", "nonce")
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with missing signature")
		}
		if err == nil || err.Error() != "missing signature header" {
			t.Errorf("expected missing signature header error, got: %v", err)
		}
	})

	t.Run("missing timestamp header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "sig")
		req.Header.Set("X-Nonce", "nonce")
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with missing timestamp")
		}
		if err == nil || err.Error() != "missing timestamp header" {
			t.Errorf("expected missing timestamp header error, got: %v", err)
		}
	})

	t.Run("missing nonce header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "sig")
		req.Header.Set("X-Timestamp", "12345")
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with missing nonce")
		}
		if err == nil || err.Error() != "missing nonce header" {
			t.Errorf("expected missing nonce header error, got: %v", err)
		}
	})

	t.Run("missing key ID header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "sig")
		req.Header.Set("X-Timestamp", "12345")
		req.Header.Set("X-Nonce", "nonce")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with missing key ID")
		}
		if err == nil || err.Error() != "missing key ID header" {
			t.Errorf("expected missing key ID header error, got: %v", err)
		}
	})

	t.Run("invalid timestamp format", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "sig")
		req.Header.Set("X-Timestamp", "invalid")
		req.Header.Set("X-Nonce", "nonce")
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with malformed timestamp")
		}
		if err == nil {
			t.Error("expected error for invalid timestamp")
		}
	})

	t.Run("timestamp skew too large", func(t *testing.T) {
		// Use timestamp that's too old
		timestamp := time.Now().Unix() - int64(10*time.Minute.Seconds())
		nonce := "nonce-old-timestamp"

		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "sig")
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with old timestamp")
		}
		if err == nil || err.Error() != "timestamp skew too large" {
			t.Errorf("expected timestamp skew error, got: %v", err)
		}
	})

	t.Run("unknown key ID", func(t *testing.T) {
		timestamp := time.Now().Unix()
		nonce := "nonce-unknown-key"

		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "sig")
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "unknown-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with unknown key")
		}
		if err == nil || err.Error() != "unknown key ID" {
			t.Errorf("expected unknown key ID error, got: %v", err)
		}
	})

	t.Run("signature mismatch", func(t *testing.T) {
		timestamp := time.Now().Unix()
		nonce := "nonce-mismatch"

		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.Header.Set("X-Signature", "badsignature123456789012345678901234567890")
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if valid {
			t.Error("should be invalid with wrong signature")
		}
		if err == nil || err.Error() != "signature mismatch" {
			t.Errorf("expected signature mismatch error, got: %v", err)
		}
	})

	t.Run("disabled verification", func(t *testing.T) {
		cfg := DefaultSignatureConfig()
		cfg.Enabled = false
		sv := NewSignatureVerifier(cfg)

		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		// No signature headers needed when disabled

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("unexpected error when disabled: %v", err)
		}
		if !valid {
			t.Error("should always be valid when disabled")
		}
	})
}

func TestReplayPrevention(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	timestamp := time.Now().Unix()
	nonce := "nonce-replay-test"
	method := "GET"
	path := "/api/v1/test"
	body := ""

	// Generate signature
	bodyHash := sv.computeBodyHash(body)
	signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"test-key", timestamp, nonce, method, path, bodyHash)
	sig := sv.computeHMAC(signData, "test-secret", "SHA256")

	// First request should succeed
	req1 := httptest.NewRequest("GET", path, nil)
	req1.Header.Set("X-Signature", sig)
	req1.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req1.Header.Set("X-Nonce", nonce)
	req1.Header.Set("X-Key-ID", "test-key")

	valid, err := sv.Verify(req1)
	if err != nil {
		t.Errorf("first request failed: %v", err)
	}
	if !valid {
		t.Error("first request should be valid")
	}

	// Second request with same nonce should fail (replay attack)
	req2 := httptest.NewRequest("GET", path, nil)
	req2.Header.Set("X-Signature", sig)
	req2.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req2.Header.Set("X-Nonce", nonce) // Same nonce
	req2.Header.Set("X-Key-ID", "test-key")

	valid, err = sv.Verify(req2)
	if valid {
		t.Error("replay request should be invalid")
	}
	if err == nil || err.Error() != "replay attack detected: nonce already used" {
		t.Errorf("expected replay attack error, got: %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)

	// Start with key1
	sv.AddSecret("key1", "secret1")

	timestamp := time.Now().Unix()
	nonce := "nonce-rotation"
	method := "GET"
	path := "/api/v1/test"
	body := ""

	// Generate signature with key1
	bodyHash := sv.computeBodyHash(body)
	signData1 := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"key1", timestamp, nonce, method, path, bodyHash)
	sig1 := sv.computeHMAC(signData1, "secret1", "SHA256")

	req1 := httptest.NewRequest("GET", path, nil)
	req1.Header.Set("X-Signature", sig1)
	req1.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req1.Header.Set("X-Nonce", nonce)
	req1.Header.Set("X-Key-ID", "key1")

	valid, err := sv.Verify(req1)
	if err != nil {
		t.Errorf("request with key1 failed: %v", err)
	}
	if !valid {
		t.Error("request with key1 should be valid")
	}

	// Rotate to key2, remove key1
	sv.AddSecret("key2", "secret2")
	sv.RemoveSecret("key1")

	// New request with key2
	nonce2 := "nonce-rotation-2"
	signData2 := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"key2", timestamp, nonce2, method, path, bodyHash)
	sig2 := sv.computeHMAC(signData2, "secret2", "SHA256")

	req2 := httptest.NewRequest("GET", path, nil)
	req2.Header.Set("X-Signature", sig2)
	req2.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req2.Header.Set("X-Nonce", nonce2)
	req2.Header.Set("X-Key-ID", "key2")

	valid, err = sv.Verify(req2)
	if err != nil {
		t.Errorf("request with key2 failed: %v", err)
	}
	if !valid {
		t.Error("request with key2 should be valid")
	}

	// Old key should no longer work
	nonce3 := "nonce-rotation-3"
	req3 := httptest.NewRequest("GET", path, nil)
	req3.Header.Set("X-Signature", sig1)
	req3.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req3.Header.Set("X-Nonce", nonce3)
	req3.Header.Set("X-Key-ID", "key1")

	valid, err = sv.Verify(req3)
	if valid {
		t.Error("request with removed key1 should be invalid")
	}
	if err == nil || err.Error() != "unknown key ID" {
		t.Errorf("expected unknown key ID error, got: %v", err)
	}
}

func TestMultipleAlgorithms(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	cfg.Algorithms = []string{"SHA256", "SHA512"}
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	t.Run("SHA256 signature", func(t *testing.T) {
		timestamp := time.Now().Unix()
		nonce := "nonce-sha256"
		method := "GET"
		path := "/api/v1/test"
		body := ""

		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA256")

		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("SHA256 verification failed: %v", err)
		}
		if !valid {
			t.Error("SHA256 signature should be valid")
		}
	})

	t.Run("SHA512 signature", func(t *testing.T) {
		timestamp := time.Now().Unix()
		nonce := "nonce-sha512"
		method := "GET"
		path := "/api/v1/test"
		body := ""

		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA512")

		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("SHA512 verification failed: %v", err)
		}
		if !valid {
			t.Error("SHA512 signature should be valid")
		}
	})
}

func TestNonceCache(t *testing.T) {
	t.Run("add and check duplicates", func(t *testing.T) {
		nc := newNonceCache(100)

		// First add should succeed
		if !nc.Add("nonce1", 1000) {
			t.Error("first add should succeed")
		}

		// Duplicate add should fail
		if nc.Add("nonce1", 1000) {
			t.Error("duplicate add should fail")
		}

		// Different nonce should succeed
		if !nc.Add("nonce2", 1000) {
			t.Error("different nonce should succeed")
		}
	})

	t.Run("cleanup on overflow", func(t *testing.T) {
		nc := newNonceCache(5)

		// Add entries with old timestamps
		for i := 0; i < 5; i++ {
			nc.Add(fmt.Sprintf("old-nonce-%d", i), 1000)
		}

		// Add new entry, which should trigger cleanup of old entries
		// The cleanup removes entries older than 1 hour
		currentTime := int64(3600 + 1000) // 1 hour + some
		if !nc.Add("new-nonce", currentTime) {
			t.Error("add after cleanup should succeed")
		}

		// Old nonces should be cleaned up, so we can add them again
		// Note: This depends on the cleanup logic running
	})

	t.Run("concurrent access", func(t *testing.T) {
		nc := newNonceCache(100)
		done := make(chan bool)

		// Concurrent writes
		for i := 0; i < 10; i++ {
			go func(id int) {
				for j := 0; j < 100; j++ {
					nc.Add(fmt.Sprintf("nonce-%d-%d", id, j), int64(j))
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}
	})
}

func TestSignWithKeyID(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())
	sv.AddSecret("key1", "secret1")
	sv.AddSecret("key2", "secret2")

	timestamp := time.Now().Unix()
	nonce := "nonce-sign-with-key"
	method := "POST"
	path := "/api/v1/test"
	body := "test body"

	sig1 := sv.SignWithKeyID(method, path, body, timestamp, nonce, "key1")
	sig2 := sv.SignWithKeyID(method, path, body, timestamp, nonce, "key2")

	// Different keys should produce different signatures
	if sig1 == sig2 {
		t.Error("different keys should produce different signatures")
	}

	// Non-existent key should return empty
	sig3 := sv.SignWithKeyID(method, path, body, timestamp, nonce, "nonexistent")
	if sig3 != "" {
		t.Error("non-existent key should return empty signature")
	}
}

func TestGetConfig(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	cfg.MaxTimestampSkew = 10 * time.Minute
	sv := NewSignatureVerifier(cfg)

	retrievedCfg := sv.GetConfig()
	if retrievedCfg.Enabled != true {
		t.Error("config should be enabled")
	}
	if retrievedCfg.MaxTimestampSkew != 10*time.Minute {
		t.Errorf("expected 10 minute skew, got %v", retrievedCfg.MaxTimestampSkew)
	}
}

func TestVerifyWithRequestPath(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	timestamp := time.Now().Unix()
	nonce := "nonce-path-test"
	method := "GET"
	path := "/api/v1/users/123"
	body := ""

	bodyHash := sv.computeBodyHash(body)
	signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"test-key", timestamp, nonce, method, path, bodyHash)
	sig := sv.computeHMAC(signData, "test-secret", "SHA256")

	req := httptest.NewRequest("GET", "/api/v1/users/123", nil)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Key-ID", "test-key")

	valid, err := sv.Verify(req)
	if err != nil {
		t.Errorf("path verification failed: %v", err)
	}
	if !valid {
		t.Error("signature with path should be valid")
	}
}

func TestVerifyWithQueryParams(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	timestamp := time.Now().Unix()
	nonce := "nonce-query-test"
	method := "GET"
	// Note: URL.Path doesn't include query string
	path := "/api/v1/search"
	body := ""

	bodyHash := sv.computeBodyHash(body)
	signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"test-key", timestamp, nonce, method, path, bodyHash)
	sig := sv.computeHMAC(signData, "test-secret", "SHA256")

	req := httptest.NewRequest("GET", "/api/v1/search?q=test&limit=10", nil)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Key-ID", "test-key")

	valid, err := sv.Verify(req)
	if err != nil {
		t.Errorf("query params verification failed: %v", err)
	}
	if !valid {
		t.Error("signature with query params should be valid")
	}
}

func TestFutureTimestamp(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	// Use a future timestamp
	timestamp := time.Now().Unix() + int64(10*time.Minute.Seconds())
	nonce := "nonce-future"

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("X-Signature", "sig")
	req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Key-ID", "test-key")

	valid, err := sv.Verify(req)
	if valid {
		t.Error("future timestamp should be invalid")
	}
	if err == nil || err.Error() != "timestamp skew too large" {
		t.Errorf("expected timestamp skew error for future timestamp, got: %v", err)
	}
}

func TestMultipleSecretsWithSameRequest(t *testing.T) {
	cfg := DefaultSignatureConfig()
	cfg.Enabled = true
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("key1", "secret1")
	sv.AddSecret("key2", "secret2")

	// Create a valid request signed with key1
	timestamp := time.Now().Unix()
	nonce := "nonce-multi-key"
	method := "GET"
	path := "/api/v1/test"
	body := ""

	bodyHash := sv.computeBodyHash(body)
	signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"key1", timestamp, nonce, method, path, bodyHash)
	sig := sv.computeHMAC(signData, "secret1", "SHA256")

	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Key-ID", "key1")

	valid, err := sv.Verify(req)
	if err != nil {
		t.Errorf("verification failed: %v", err)
	}
	if !valid {
		t.Error("request should be valid")
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		cfg := DefaultSignatureConfig()
		cfg.Enabled = true
		sv := NewSignatureVerifier(cfg)
		sv.AddSecret("test-key", "test-secret")

		timestamp := time.Now().Unix()
		nonce := "nonce-empty-path"
		method := "GET"
		path := "/"
		body := ""

		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA256")

		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("empty path verification failed: %v", err)
		}
		if !valid {
			t.Error("signature with empty path should be valid")
		}
	})

	t.Run("unicode body", func(t *testing.T) {
		cfg := DefaultSignatureConfig()
		cfg.Enabled = true
		sv := NewSignatureVerifier(cfg)
		sv.AddSecret("test-key", "test-secret")

		timestamp := time.Now().Unix()
		nonce := "nonce-unicode"
		method := "POST"
		path := "/api/v1/test"
		body := `{"message":"你好世界🎉"}`

		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA256")

		req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("unicode body verification failed: %v", err)
		}
		if !valid {
			t.Error("signature with unicode body should be valid")
		}
	})

	t.Run("large body", func(t *testing.T) {
		cfg := DefaultSignatureConfig()
		cfg.Enabled = true
		sv := NewSignatureVerifier(cfg)
		sv.AddSecret("test-key", "test-secret")

		// Create a large body (1MB)
		body := string(make([]byte, 1024*1024))
		for i := range body {
			body = string(rune(i % 256))
		}
		body = "" // Use simpler large body
		for i := 0; i < 10000; i++ {
			body += "test data line " + string(rune(i)) + "\n"
		}

		timestamp := time.Now().Unix()
		nonce := "nonce-large"
		method := "POST"
		path := "/api/v1/upload"

		bodyHash := sv.computeBodyHash(body)
		signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			"test-key", timestamp, nonce, method, path, bodyHash)
		sig := sv.computeHMAC(signData, "test-secret", "SHA256")

		req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		req.Header.Set("X-Signature", sig)
		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Key-ID", "test-key")

		valid, err := sv.Verify(req)
		if err != nil {
			t.Errorf("large body verification failed: %v", err)
		}
		if !valid {
			t.Error("signature with large body should be valid")
		}
	})
}

func TestCustomHeaders(t *testing.T) {
	cfg := SignatureConfig{
		Enabled:          true,
		HeaderName:       "X-Custom-Signature",
		TimestampHeader:  "X-Custom-Timestamp",
		NonceHeader:      "X-Custom-Nonce",
		KeyIDHeader:      "X-Custom-KeyID",
		MaxTimestampSkew: 5 * time.Minute,
		NonceCacheSize:   10000,
		Algorithms:       []string{"SHA256"},
	}
	sv := NewSignatureVerifier(cfg)
	sv.AddSecret("test-key", "test-secret")

	timestamp := time.Now().Unix()
	nonce := "nonce-custom-headers"
	method := "GET"
	path := "/api/v1/test"
	body := ""

	bodyHash := sv.computeBodyHash(body)
	signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"test-key", timestamp, nonce, method, path, bodyHash)
	sig := sv.computeHMAC(signData, "test-secret", "SHA256")

	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("X-Custom-Signature", sig)
	req.Header.Set("X-Custom-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Custom-Nonce", nonce)
	req.Header.Set("X-Custom-KeyID", "test-key")

	valid, err := sv.Verify(req)
	if err != nil {
		t.Errorf("custom headers verification failed: %v", err)
	}
	if !valid {
		t.Error("signature with custom headers should be valid")
	}
}

func TestSignWithNoSecret(t *testing.T) {
	sv := NewSignatureVerifier(DefaultSignatureConfig())
	// No secrets added

	sig := sv.Sign("GET", "/api/v1/test", "", time.Now().Unix(), "nonce")
	if sig != "" {
		t.Error("signing with no secret should return empty string")
	}
}