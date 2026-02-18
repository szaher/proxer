package gateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type TLSCertificate struct {
	ID              string    `json:"id"`
	Hostname        string    `json:"hostname"`
	CertPEM         string    `json:"cert_pem,omitempty"`
	KeyPEMEncrypted string    `json:"key_pem_encrypted,omitempty"`
	Active          bool      `json:"active"`
	ExpiresAt       time.Time `json:"expires_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type TLSCertificateInput struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	CertPEM  string `json:"cert_pem"`
	KeyPEM   string `json:"key_pem"`
	Active   bool   `json:"active"`
}

type tlsCertificateRecord struct {
	meta    TLSCertificate
	certPEM string
	keyEnc  string
}

type TLSStore struct {
	mu   sync.RWMutex
	key  []byte
	cert map[string]tlsCertificateRecord
}

func NewTLSStore(encryptionKey string) *TLSStore {
	encryptionKey = strings.TrimSpace(encryptionKey)
	var keyBytes []byte
	if encryptionKey != "" {
		sum := sha256.Sum256([]byte("proxer-tls:" + encryptionKey))
		keyBytes = sum[:]
	}
	return &TLSStore{
		key:  keyBytes,
		cert: make(map[string]tlsCertificateRecord),
	}
}

func (s *TLSStore) List() []TLSCertificate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]TLSCertificate, 0, len(s.cert))
	for _, record := range s.cert {
		meta := record.meta
		meta.CertPEM = ""
		meta.KeyPEMEncrypted = record.keyEnc
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hostname == out[j].Hostname {
			return out[i].ID < out[j].ID
		}
		return out[i].Hostname < out[j].Hostname
	})
	return out
}

func (s *TLSStore) Get(id string) (TLSCertificate, bool) {
	id = normalizeIdentifier(id)
	if id == "" {
		return TLSCertificate{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.cert[id]
	if !ok {
		return TLSCertificate{}, false
	}
	meta := record.meta
	meta.CertPEM = ""
	meta.KeyPEMEncrypted = record.keyEnc
	return meta, true
}

func (s *TLSStore) Upsert(input TLSCertificateInput) (TLSCertificate, error) {
	id := normalizeIdentifier(input.ID)
	if !identifierPattern.MatchString(id) {
		return TLSCertificate{}, fmt.Errorf("invalid certificate id %q", id)
	}
	hostname := strings.ToLower(strings.TrimSpace(input.Hostname))
	if hostname == "" {
		return TLSCertificate{}, fmt.Errorf("hostname is required")
	}
	certPEM := strings.TrimSpace(input.CertPEM)
	keyPEM := strings.TrimSpace(input.KeyPEM)
	if certPEM == "" || keyPEM == "" {
		return TLSCertificate{}, fmt.Errorf("cert_pem and key_pem are required")
	}

	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return TLSCertificate{}, fmt.Errorf("invalid cert/key pair: %w", err)
	}
	expiresAt, err := certificateExpiry(tlsCert)
	if err != nil {
		return TLSCertificate{}, err
	}
	encKey, err := s.encryptKey(keyPEM)
	if err != nil {
		return TLSCertificate{}, err
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.cert[id]
	if !ok {
		record.meta.CreatedAt = now
	}
	record.meta.ID = id
	record.meta.Hostname = hostname
	record.meta.Active = input.Active
	record.meta.ExpiresAt = expiresAt
	record.meta.UpdatedAt = now
	record.certPEM = certPEM
	record.keyEnc = encKey
	s.cert[id] = record

	if input.Active {
		s.deactivateOthersForHostLocked(id, hostname)
	}

	meta := record.meta
	meta.CertPEM = ""
	meta.KeyPEMEncrypted = encKey
	return meta, nil
}

func (s *TLSStore) SetActive(id string, active bool) (TLSCertificate, error) {
	id = normalizeIdentifier(id)
	if id == "" {
		return TLSCertificate{}, fmt.Errorf("missing certificate id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.cert[id]
	if !ok {
		return TLSCertificate{}, fmt.Errorf("certificate %q not found", id)
	}
	record.meta.Active = active
	record.meta.UpdatedAt = time.Now().UTC()
	s.cert[id] = record
	if active {
		s.deactivateOthersForHostLocked(id, record.meta.Hostname)
	}
	meta := record.meta
	meta.CertPEM = ""
	meta.KeyPEMEncrypted = record.keyEnc
	return meta, nil
}

func (s *TLSStore) Delete(id string) bool {
	id = normalizeIdentifier(id)
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cert[id]; !ok {
		return false
	}
	delete(s.cert, id)
	return true
}

func (s *TLSStore) ActiveCertificateCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, record := range s.cert {
		if record.meta.Active {
			count++
		}
	}
	return count
}

func (s *TLSStore) CertificateForHostname(hostname string) (*tls.Certificate, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))

	s.mu.RLock()
	defer s.mu.RUnlock()

	if hostname == "" {
		for _, record := range s.cert {
			if !record.meta.Active {
				continue
			}
			keyPEM, err := s.decryptKey(record.keyEnc)
			if err != nil {
				return nil, err
			}
			cert, err := tls.X509KeyPair([]byte(record.certPEM), []byte(keyPEM))
			if err != nil {
				return nil, err
			}
			return &cert, nil
		}
		return nil, fmt.Errorf("no active certificates configured")
	}

	for _, record := range s.cert {
		if !record.meta.Active || !hostMatches(record.meta.Hostname, hostname) {
			continue
		}
		keyPEM, err := s.decryptKey(record.keyEnc)
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair([]byte(record.certPEM), []byte(keyPEM))
		if err != nil {
			return nil, err
		}
		return &cert, nil
	}
	return nil, fmt.Errorf("no active certificate for host %q", hostname)
}

func (s *TLSStore) deactivateOthersForHostLocked(activeID, hostname string) {
	for id, record := range s.cert {
		if id == activeID {
			continue
		}
		if record.meta.Hostname != hostname {
			continue
		}
		record.meta.Active = false
		record.meta.UpdatedAt = time.Now().UTC()
		s.cert[id] = record
	}
}

func (s *TLSStore) encryptKey(raw string) (string, error) {
	if len(s.key) == 0 {
		return "plain:" + base64.StdEncoding.EncodeToString([]byte(raw)), nil
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, []byte(raw), nil)
	combined := append(nonce, ciphertext...)
	return "enc:" + base64.StdEncoding.EncodeToString(combined), nil
}

func (s *TLSStore) decryptKey(encoded string) (string, error) {
	if strings.HasPrefix(encoded, "plain:") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "plain:"))
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	if !strings.HasPrefix(encoded, "enc:") {
		return "", fmt.Errorf("unknown key encoding")
	}
	if len(s.key) == 0 {
		return "", fmt.Errorf("tls key encryption key is not configured")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aead.NonceSize()
	if len(payload) <= nonceSize {
		return "", fmt.Errorf("encrypted payload too short")
	}
	nonce, ciphertext := payload[:nonceSize], payload[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func hostMatches(expected, actual string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	actual = strings.ToLower(strings.TrimSpace(actual))
	if expected == "" || actual == "" {
		return false
	}
	if expected == actual {
		return true
	}
	if strings.HasPrefix(expected, "*.") {
		suffix := strings.TrimPrefix(expected, "*")
		return strings.HasSuffix(actual, suffix)
	}
	return false
}

func certificateExpiry(cert tls.Certificate) (time.Time, error) {
	if len(cert.Certificate) == 0 {
		return time.Time{}, fmt.Errorf("certificate chain is empty")
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err == nil {
		return parsed.NotAfter.UTC(), nil
	}
	block, _ := pem.Decode(cert.Certificate[0])
	if block != nil {
		parsed, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr == nil {
			return parsed.NotAfter.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse certificate: %w", err)
}
