package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test helper functions

func generateTestCertificate(t *testing.T, commonName string, organizationalUnit string, notBefore, notAfter time.Time) ([]byte, []byte, *x509.Certificate) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:         commonName,
			OrganizationalUnit: []string{organizationalUnit},
			Organization:        []string{"Test Org"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	return certPEM, keyPEM, cert
}

func generateTestCA(t *testing.T) ([]byte, []byte, *x509.Certificate) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate CA private key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Test CA Org"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	return certPEM, keyPEM, cert
}

func createTempFile(t *testing.T, data []byte) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.pem")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	return tmpFile
}

// Tests

func TestNewTLSCertManager(t *testing.T) {
	t.Run("with nil config", func(t *testing.T) {
		mgr := NewTLSCertManager(nil)
		if mgr == nil {
			t.Fatal("expected manager to be created")
		}
		if mgr.IsEnabled() {
			t.Error("expected TLS to be disabled with nil config")
		}
	})

	t.Run("with disabled config", func(t *testing.T) {
		cfg := &TLSConfig{Enabled: false}
		mgr := NewTLSCertManager(cfg)
		if mgr.IsEnabled() {
			t.Error("expected TLS to be disabled")
		}
	})

	t.Run("with enabled config", func(t *testing.T) {
		cfg := &TLSConfig{Enabled: true}
		mgr := NewTLSCertManager(cfg)
		if !mgr.IsEnabled() {
			t.Error("expected TLS to be enabled")
		}
	})
}

func TestTLSCertManager_LoadCertificates(t *testing.T) {
	t.Run("successful load", func(t *testing.T) {
		certPEM, keyPEM, _ := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		caPEM, _, _ := generateTestCA(t)

		certFile := createTempFile(t, certPEM)
		keyFile := createTempFile(t, keyPEM)
		caFile := createTempFile(t, caPEM)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCertificates(certFile, keyFile, caFile)
		if err != nil {
			t.Fatalf("Failed to load certificates: %v", err)
		}
	})

	t.Run("certificate file not found", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCertificates("/nonexistent/cert.pem", "", "")
		if err != ErrCertFileNotFound {
			t.Errorf("expected ErrCertFileNotFound, got %v", err)
		}
	})

	t.Run("key file not found", func(t *testing.T) {
		certPEM, _, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		certFile := createTempFile(t, certPEM)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCertificates(certFile, "/nonexistent/key.pem", "")
		if err != ErrKeyFileNotFound {
			t.Errorf("expected ErrKeyFileNotFound, got %v", err)
		}
	})

	t.Run("CA file not found", func(t *testing.T) {
		certPEM, keyPEM, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		certFile := createTempFile(t, certPEM)
		keyFile := createTempFile(t, keyPEM)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCertificates(certFile, keyFile, "/nonexistent/ca.pem")
		if err != ErrCAFileNotFound {
			t.Errorf("expected ErrCAFileNotFound, got %v", err)
		}
	})

	t.Run("empty paths", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCertificates("", "", "")
		if err != nil {
			t.Errorf("expected no error for empty paths, got %v", err)
		}
	})
}

func TestTLSCertManager_GetTLSConfig(t *testing.T) {
	t.Run("disabled TLS returns nil", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: false})
		cfg := mgr.GetTLSConfig()
		if cfg != nil {
			t.Error("expected nil config when TLS is disabled")
		}
	})

	t.Run("enabled TLS returns config", func(t *testing.T) {
		certPEM, keyPEM, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		certFile := createTempFile(t, certPEM)
		keyFile := createTempFile(t, keyPEM)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		_ = mgr.LoadCertificates(certFile, keyFile, "")

		cfg := mgr.GetTLSConfig()
		if cfg == nil {
			t.Fatal("expected non-nil config when TLS is enabled")
		}

		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("expected MinVersion TLS 1.2, got %v", cfg.MinVersion)
		}
	})

	t.Run("with client auth", func(t *testing.T) {
		certPEM, keyPEM, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		caPEM, _, _ := generateTestCA(t)

		certFile := createTempFile(t, certPEM)
		keyFile := createTempFile(t, keyPEM)
		caFile := createTempFile(t, caPEM)

		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			ClientAuth: tls.RequireAndVerifyClientCert,
		})
		_ = mgr.LoadCertificates(certFile, keyFile, caFile)

		cfg := mgr.GetTLSConfig()
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}

		if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
			t.Errorf("expected RequireAndVerifyClientCert, got %v", cfg.ClientAuth)
		}

		if cfg.ClientCAs == nil {
			t.Error("expected ClientCAs to be set")
		}
	})

	t.Run("client auth without CA pool", func(t *testing.T) {
		certPEM, keyPEM, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		certFile := createTempFile(t, certPEM)
		keyFile := createTempFile(t, keyPEM)

		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			ClientAuth: tls.RequireAndVerifyClientCert,
		})
		_ = mgr.LoadCertificates(certFile, keyFile, "")

		cfg := mgr.GetTLSConfig()
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}

		// Without CA pool, should fall back to NoClientCert
		if cfg.ClientAuth != tls.NoClientCert {
			t.Errorf("expected NoClientCert when no CA pool, got %v", cfg.ClientAuth)
		}
	})

	t.Run("custom TLS versions", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13,
		})

		cfg := mgr.GetTLSConfig()
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}

		if cfg.MinVersion != tls.VersionTLS13 {
			t.Errorf("expected MinVersion TLS 1.3, got %v", cfg.MinVersion)
		}

		if cfg.MaxVersion != tls.VersionTLS13 {
			t.Errorf("expected MaxVersion TLS 1.3, got %v", cfg.MaxVersion)
		}
	})
}

func TestTLSCertManager_VerifyClientCert(t *testing.T) {
	t.Run("empty certificates", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.VerifyClientCert(nil)
		if err != ErrNoCertificates {
			t.Errorf("expected ErrNoCertificates, got %v", err)
		}
	})

	t.Run("valid certificate", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.VerifyClientCert([][]byte{cert.Raw})
		if err != nil {
			t.Errorf("expected no error for valid certificate, got %v", err)
		}
	})

	t.Run("expired certificate", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.VerifyClientCert([][]byte{cert.Raw})
		if err == nil {
			t.Error("expected error for expired certificate")
		}
	})

	t.Run("not yet valid certificate", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(24*time.Hour), time.Now().Add(48*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.VerifyClientCert([][]byte{cert.Raw})
		if err == nil {
			t.Error("expected error for not yet valid certificate")
		}
	})

	t.Run("invalid certificate data", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.VerifyClientCert([][]byte{[]byte("invalid cert data")})
		if err == nil {
			t.Error("expected error for invalid certificate data")
		}
	})
}

func TestTLSCertManager_GetClientIdentity(t *testing.T) {
	t.Run("extract CN and OU", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-common-name", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})

		cn, ou := mgr.GetClientIdentity(cert)
		if cn != "test-common-name" {
			t.Errorf("expected CN 'test-common-name', got %s", cn)
		}
		if ou != "test-ou" {
			t.Errorf("expected OU 'test-ou', got %s", ou)
		}
	})

	t.Run("nil certificate", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		cn, ou := mgr.GetClientIdentity(nil)
		if cn != "" || ou != "" {
			t.Errorf("expected empty strings for nil cert, got cn=%s, ou=%s", cn, ou)
		}
	})

	t.Run("empty identity fields", func(t *testing.T) {
		privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("Failed to generate key: %v", err)
		}

		template := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{}, // Empty subject
			NotBefore:             time.Now().Add(-24 * time.Hour),
			NotAfter:              time.Now().Add(365 * 24 * time.Hour),
			KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
		}

		certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
		cert, _ := x509.ParseCertificate(certDER)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		cn, ou := mgr.GetClientIdentity(cert)
		if cn != "" || ou != "" {
			t.Errorf("expected empty strings for empty subject, got cn=%s, ou=%s", cn, ou)
		}
	})
}

func TestTLSCertManager_AllowedCNs(t *testing.T) {
	t.Run("CN in allowed list", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "allowed-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedCNs: []string{"allowed-client", "another-client"},
		})

		err := mgr.ValidateClientCertificate(cert)
		if err != nil {
			t.Errorf("expected no error for allowed CN, got %v", err)
		}
	})

	t.Run("CN not in allowed list", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "unknown-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedCNs: []string{"allowed-client", "another-client"},
		})

		err := mgr.ValidateClientCertificate(cert)
		if err == nil {
			t.Error("expected error for CN not in allowed list")
		}
	})

	t.Run("no CN restriction", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "any-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedCNs: []string{}, // Empty means no restriction
		})

		err := mgr.ValidateClientCertificate(cert)
		if err != nil {
			t.Errorf("expected no error when no CN restriction, got %v", err)
		}
	})
}

func TestTLSCertManager_AllowedOUs(t *testing.T) {
	t.Run("OU in allowed list", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "allowed-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedOUs: []string{"allowed-ou", "another-ou"},
		})

		err := mgr.ValidateClientCertificate(cert)
		if err != nil {
			t.Errorf("expected no error for allowed OU, got %v", err)
		}
	})

	t.Run("OU not in allowed list", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "unknown-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedOUs: []string{"allowed-ou", "another-ou"},
		})

		err := mgr.ValidateClientCertificate(cert)
		if err == nil {
			t.Error("expected error for OU not in allowed list")
		}
	})
}

func TestTLSCertManager_Reload(t *testing.T) {
	t.Run("successful reload", func(t *testing.T) {
		certPEM, keyPEM, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		caPEM, _, _ := generateTestCA(t)

		certFile := createTempFile(t, certPEM)
		keyFile := createTempFile(t, keyPEM)
		caFile := createTempFile(t, caPEM)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCertificates(certFile, keyFile, caFile)
		if err != nil {
			t.Fatalf("Failed to load certificates: %v", err)
		}

		// Reload should succeed
		err = mgr.Reload()
		if err != nil {
			t.Errorf("Failed to reload: %v", err)
		}
	})

	t.Run("reload with non-existent files", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  "/nonexistent/key.pem",
		})

		err := mgr.Reload()
		if err == nil {
			t.Error("expected error when reloading with non-existent files")
		}
	})
}

func TestTLSCertManager_AddAllowedCN(t *testing.T) {
	mgr := NewTLSCertManager(&TLSConfig{Enabled: true})

	mgr.AddAllowedCN("new-client")

	cfg := mgr.GetConfig()
	found := false
	for _, cn := range cfg.AllowedCNs {
		if cn == "new-client" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected new-client to be in allowed CNs")
	}
}

func TestTLSCertManager_AddAllowedOU(t *testing.T) {
	mgr := NewTLSCertManager(&TLSConfig{Enabled: true})

	mgr.AddAllowedOU("new-ou")

	cfg := mgr.GetConfig()
	found := false
	for _, ou := range cfg.AllowedOUs {
		if ou == "new-ou" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected new-ou to be in allowed OUs")
	}
}

func TestTLSCertManager_SetClientAuth(t *testing.T) {
	mgr := NewTLSCertManager(&TLSConfig{Enabled: true})

	mgr.SetClientAuth(tls.RequireAndVerifyClientCert)

	cfg := mgr.GetConfig()
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("expected RequireAndVerifyClientCert, got %v", cfg.ClientAuth)
	}
}

func TestTLSCertManager_ValidateClientCertificate(t *testing.T) {
	t.Run("nil certificate", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.ValidateClientCertificate(nil)
		if err != ErrInvalidCertificate {
			t.Errorf("expected ErrInvalidCertificate, got %v", err)
		}
	})

	t.Run("valid certificate", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})

		err := mgr.ValidateClientCertificate(cert)
		if err != nil {
			t.Errorf("expected no error for valid certificate, got %v", err)
		}
	})

	t.Run("expired certificate", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})

		err := mgr.ValidateClientCertificate(cert)
		if err == nil {
			t.Error("expected error for expired certificate")
		}
	})

	t.Run("certificate with CN restriction", func(t *testing.T) {
		_, _, cert := generateTestCertificate(t, "allowed-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedCNs: []string{"allowed-client"},
		})

		err := mgr.ValidateClientCertificate(cert)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestTLSCertManager_GetCACertPool(t *testing.T) {
	t.Run("no CA loaded", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		pool := mgr.GetCACertPool()
		if pool != nil {
			t.Error("expected nil CA pool when no CA loaded")
		}
	})

	t.Run("CA loaded", func(t *testing.T) {
		caPEM, _, _ := generateTestCA(t)
		caFile := createTempFile(t, caPEM)

		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		_ = mgr.LoadCertificates("", "", caFile)

		pool := mgr.GetCACertPool()
		if pool == nil {
			t.Error("expected non-nil CA pool after loading CA")
		}
	})
}

func TestTLSCertManager_CRL(t *testing.T) {
	t.Run("LoadCRL with non-existent file", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCRL("/nonexistent/crl.pem")
		if err == nil {
			t.Error("expected error for non-existent CRL file")
		}
	})

	t.Run("LoadCRLFromBytes with invalid PEM", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		err := mgr.LoadCRLFromBytes([]byte("not valid PEM"))
		if err == nil {
			t.Error("expected error for invalid PEM")
		}
	})

	t.Run("LoadCRLFromBytes with non-CRL PEM", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		// Create a certificate PEM, not a CRL
		certPEM, _, _ := generateTestCertificate(t, "test", "ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))
		err := mgr.LoadCRLFromBytes(certPEM)
		if err == nil {
			t.Error("expected error for non-CRL PEM data")
		}
	})
}

func TestTLSCertManager_verifyConnection(t *testing.T) {
	t.Run("no peer certificates with VerifyClientCertIfGiven", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			ClientAuth: tls.VerifyClientCertIfGiven,
		})

		state := tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{},
		}

		err := mgr.verifyConnection(state)
		if err != ErrNoCertificates {
			t.Errorf("expected ErrNoCertificates, got %v", err)
		}
	})

	t.Run("no peer certificates with NoClientCert", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			ClientAuth: tls.NoClientCert,
		})

		state := tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{},
		}

		err := mgr.verifyConnection(state)
		if err != nil {
			t.Errorf("expected no error with NoClientCert, got %v", err)
		}
	})

	t.Run("valid peer certificate", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))

		state := tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
		}

		err := mgr.verifyConnection(state)
		if err != nil {
			t.Errorf("expected no error for valid certificate, got %v", err)
		}
	})

	t.Run("peer certificate with CN restriction", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedCNs: []string{"allowed-client"},
		})
		_, _, cert := generateTestCertificate(t, "allowed-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))

		state := tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
		}

		err := mgr.verifyConnection(state)
		if err != nil {
			t.Errorf("expected no error for allowed CN, got %v", err)
		}
	})

	t.Run("peer certificate with CN not allowed", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{
			Enabled:    true,
			AllowedCNs: []string{"other-client"},
		})
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour))

		state := tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
		}

		err := mgr.verifyConnection(state)
		if err == nil {
			t.Error("expected error for CN not in allowed list")
		}
	})

	t.Run("expired peer certificate", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		_, _, cert := generateTestCertificate(t, "test-client", "test-ou", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))

		state := tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
		}

		err := mgr.verifyConnection(state)
		if err == nil {
			t.Error("expected error for expired certificate")
		}
	})
}

func TestTLSCertManager_GetConfig(t *testing.T) {
	cfg := &TLSConfig{
		Enabled:    true,
		ClientAuth: tls.RequireAndVerifyClientCert,
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		AllowedCNs: []string{"client1"},
		AllowedOUs: []string{"ou1"},
	}

	mgr := NewTLSCertManager(cfg)
	returnedCfg := mgr.GetConfig()

	if returnedCfg.Enabled != cfg.Enabled {
		t.Errorf("expected Enabled %v, got %v", cfg.Enabled, returnedCfg.Enabled)
	}

	if returnedCfg.ClientAuth != cfg.ClientAuth {
		t.Errorf("expected ClientAuth %v, got %v", cfg.ClientAuth, returnedCfg.ClientAuth)
	}
}

func TestTLSCertManager_IsEnabled(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: true})
		if !mgr.IsEnabled() {
			t.Error("expected TLS to be enabled")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		mgr := NewTLSCertManager(&TLSConfig{Enabled: false})
		if mgr.IsEnabled() {
			t.Error("expected TLS to be disabled")
		}
	})
}