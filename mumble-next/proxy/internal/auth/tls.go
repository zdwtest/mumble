package auth

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Errors for TLS certificate operations
var (
	ErrTLSDisabled          = errors.New("TLS is disabled")
	ErrNoCertificates        = errors.New("no certificates provided")
	ErrCertificateExpired   = errors.New("certificate has expired")
	ErrCertificateNotValid  = errors.New("certificate not yet valid")
	ErrCertificateRevoked   = errors.New("certificate has been revoked")
	ErrInvalidCertificate   = errors.New("invalid certificate")
	ErrUnknownIssuer        = errors.New("unknown issuer")
	ErrCNNotAllowed         = errors.New("common name not in allowed list")
	ErrOUNotAllowed         = errors.New("organizational unit not in allowed list")
	ErrNoCAFile             = errors.New("CA file not specified for client verification")
	ErrCertFileNotFound     = errors.New("certificate file not found")
	ErrKeyFileNotFound      = errors.New("key file not found")
	ErrCAFileNotFound       = errors.New("CA file not found")
)

// TLSConfig holds TLS client certificate verification configuration
type TLSConfig struct {
	Enabled      bool              `mapstructure:"enabled"`
	CertFile     string            `mapstructure:"cert_file"`
	KeyFile      string            `mapstructure:"key_file"`
	CAFile       string            `mapstructure:"ca_file"`
	ClientAuth   tls.ClientAuthType `mapstructure:"client_auth"`
	MinVersion   uint16            `mapstructure:"min_version"`
	MaxVersion   uint16            `mapstructure:"max_version"`
	AllowedCNs   []string          `mapstructure:"allowed_cns"`
	AllowedOUs   []string          `mapstructure:"allowed_ous"`
}

// TLSCertManager manages TLS certificates and client verification
type TLSCertManager struct {
	config     *TLSConfig
	cert       *tls.Certificate
	caCertPool *x509.CertPool
	mu         sync.RWMutex

	// CRL (Certificate Revocation List) support
	crls       []*x509.RevocationList
	crlMu      sync.RWMutex
}

// NewTLSCertManager creates a new TLS certificate manager
func NewTLSCertManager(cfg *TLSConfig) *TLSCertManager {
	if cfg == nil {
		cfg = &TLSConfig{Enabled: false}
	}
	return &TLSCertManager{
		config: cfg,
		crls:   make([]*x509.RevocationList, 0),
	}
}

// LoadCertificates loads the server certificate, key, and CA certificates
func (m *TLSCertManager) LoadCertificates(certFile, keyFile, caFile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if files exist
	if certFile != "" {
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			return ErrCertFileNotFound
		}
	}
	if keyFile != "" {
		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			return ErrKeyFileNotFound
		}
	}

	// Load server certificate and key
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("failed to load certificate pair: %w", err)
		}
		m.cert = &cert

		// Update config paths
		m.config.CertFile = certFile
		m.config.KeyFile = keyFile
	}

	// Load CA certificate for client verification
	if caFile != "" {
		if _, err := os.Stat(caFile); os.IsNotExist(err) {
			return ErrCAFileNotFound
		}

		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("failed to parse CA certificate")
		}
		m.caCertPool = caCertPool
		m.config.CAFile = caFile
	}

	return nil
}

// GetTLSConfig returns a tls.Config configured with the loaded certificates
func (m *TLSCertManager) GetTLSConfig() *tls.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.config.Enabled {
		return nil
	}

	tlsConfig := &tls.Config{
		MinVersion: m.config.MinVersion,
		MaxVersion: m.config.MaxVersion,
	}

	// Set min/max version defaults if not specified
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}

	// Set server certificate
	if m.cert != nil {
		tlsConfig.Certificates = []tls.Certificate{*m.cert}
	}

	// Configure client authentication
	if m.config.ClientAuth != tls.NoClientCert {
		if m.caCertPool == nil {
			// Client auth requested but no CA pool available
			tlsConfig.ClientAuth = tls.NoClientCert
		} else {
			tlsConfig.ClientAuth = m.config.ClientAuth
			tlsConfig.ClientCAs = m.caCertPool

			// Set up verify callback for additional validation
			tlsConfig.VerifyConnection = m.verifyConnection
		}
	}

	return tlsConfig
}

// verifyConnection is called after the TLS handshake to perform additional verification
func (m *TLSCertManager) verifyConnection(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		if m.config.ClientAuth >= tls.VerifyClientCertIfGiven {
			return ErrNoCertificates
		}
		return nil
	}

	// Verify each peer certificate
	for _, cert := range state.PeerCertificates {
		if err := m.VerifyClientCert([][]byte{cert.Raw}); err != nil {
			return err
		}

		// Check allowed CNs and OUs
		cn, ou := m.GetClientIdentity(cert)
		if err := m.checkAllowedIdentity(cn, ou); err != nil {
			return err
		}
	}

	return nil
}

// VerifyClientCert verifies a client certificate
func (m *TLSCertManager) VerifyClientCert(rawCerts [][]byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(rawCerts) == 0 {
		return ErrNoCertificates
	}

	now := time.Now()

	for _, rawCert := range rawCerts {
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidCertificate, err)
		}

		// Check certificate validity period
		if now.Before(cert.NotBefore) {
			return fmt.Errorf("%w: certificate valid from %v", ErrCertificateNotValid, cert.NotBefore)
		}

		if now.After(cert.NotAfter) {
			return fmt.Errorf("%w: certificate expired at %v", ErrCertificateExpired, cert.NotAfter)
		}

		// Check CRL for revocation
		if err := m.checkRevocation(cert); err != nil {
			return err
		}
	}

	return nil
}

// GetClientIdentity extracts the Common Name and Organizational Unit from a certificate
func (m *TLSCertManager) GetClientIdentity(cert *x509.Certificate) (cn, ou string) {
	if cert == nil {
		return "", ""
	}

	// Extract CN from Subject
	if cert.Subject.CommonName != "" {
		cn = cert.Subject.CommonName
	}

	// Extract OU from Subject
	if len(cert.Subject.OrganizationalUnit) > 0 {
		ou = cert.Subject.OrganizationalUnit[0]
	}

	return cn, ou
}

// checkAllowedIdentity verifies the certificate identity against allowed lists
func (m *TLSCertManager) checkAllowedIdentity(cn, ou string) error {
	// Check CN against allowed list
	if len(m.config.AllowedCNs) > 0 {
		cnAllowed := false
		for _, allowedCN := range m.config.AllowedCNs {
			if cn == allowedCN {
				cnAllowed = true
				break
			}
		}
		if !cnAllowed {
			return fmt.Errorf("%w: %s", ErrCNNotAllowed, cn)
		}
	}

	// Check OU against allowed list
	if len(m.config.AllowedOUs) > 0 {
		ouAllowed := false
		for _, allowedOU := range m.config.AllowedOUs {
			if ou == allowedOU {
				ouAllowed = true
				break
			}
		}
		if !ouAllowed {
			return fmt.Errorf("%w: %s", ErrOUNotAllowed, ou)
		}
	}

	return nil
}

// checkRevocation checks if a certificate has been revoked
func (m *TLSCertManager) checkRevocation(cert *x509.Certificate) error {
	m.crlMu.RLock()
	defer m.crlMu.RUnlock()

	for _, crl := range m.crls {
		// Check certificate serial number against revoked list
		for _, revoked := range crl.RevokedCertificateEntries {
			if cert.SerialNumber.Cmp(revoked.SerialNumber) == 0 {
				return ErrCertificateRevoked
			}
		}
	}

	return nil
}

// LoadCRL loads a Certificate Revocation List from a file
func (m *TLSCertManager) LoadCRL(crlFile string) error {
	crlData, err := os.ReadFile(crlFile)
	if err != nil {
		return fmt.Errorf("failed to read CRL file: %w", err)
	}

	// Parse PEM block
	block, _ := pem.Decode(crlData)
	if block == nil {
		return fmt.Errorf("failed to parse CRL PEM block")
	}

	crl, err := x509.ParseRevocationList(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CRL: %w", err)
	}

	m.crlMu.Lock()
	defer m.crlMu.Unlock()
	m.crls = append(m.crls, crl)

	return nil
}

// LoadCRLFromBytes loads a CRL from raw bytes
func (m *TLSCertManager) LoadCRLFromBytes(crlData []byte) error {
	block, _ := pem.Decode(crlData)
	if block == nil {
		return fmt.Errorf("failed to parse CRL PEM block")
	}

	crl, err := x509.ParseRevocationList(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CRL: %w", err)
	}

	m.crlMu.Lock()
	defer m.crlMu.Unlock()
	m.crls = append(m.crls, crl)

	return nil
}

// Reload reloads all certificates from disk
func (m *TLSCertManager) Reload() error {
	m.mu.RLock()
	certFile := m.config.CertFile
	keyFile := m.config.KeyFile
	caFile := m.config.CAFile
	m.mu.RUnlock()

	return m.LoadCertificates(certFile, keyFile, caFile)
}

// IsEnabled returns whether TLS is enabled
func (m *TLSCertManager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Enabled
}

// GetCACertPool returns the CA certificate pool for client verification
func (m *TLSCertManager) GetCACertPool() *x509.CertPool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.caCertPool
}

// AddAllowedCN adds a Common Name to the allowed list
func (m *TLSCertManager) AddAllowedCN(cn string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AllowedCNs = append(m.config.AllowedCNs, cn)
}

// AddAllowedOU adds an Organizational Unit to the allowed list
func (m *TLSCertManager) AddAllowedOU(ou string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AllowedOUs = append(m.config.AllowedOUs, ou)
}

// SetClientAuth sets the client authentication mode
func (m *TLSCertManager) SetClientAuth(authType tls.ClientAuthType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ClientAuth = authType
}

// ValidateClientCertificate performs full validation of a client certificate
func (m *TLSCertManager) ValidateClientCertificate(cert *x509.Certificate) error {
	if cert == nil {
		return ErrInvalidCertificate
	}

	// Check expiry
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("%w: valid from %v", ErrCertificateNotValid, cert.NotBefore)
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("%w: expired at %v", ErrCertificateExpired, cert.NotAfter)
	}

	// Check revocation
	if err := m.checkRevocation(cert); err != nil {
		return err
	}

	// Extract and validate identity
	cn, ou := m.GetClientIdentity(cert)
	return m.checkAllowedIdentity(cn, ou)
}

// GetConfig returns the current TLS configuration
func (m *TLSCertManager) GetConfig() *TLSConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}