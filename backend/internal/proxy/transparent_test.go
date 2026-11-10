package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"red-token/internal/domain"
)

func TestTransparentProxyPreservesHostAndTLSName(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate certificate key: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "upstream.example"},
		DNSNames:              []string{"upstream.example"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create test certificate: %v", err)
	}
	rootCertificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse test certificate: %v", err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{certificateDER}, PrivateKey: key}
	serverNames := make(chan string, 2)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverNames <- r.TLS.ServerName + "|" + r.Host
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{certificate},
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			serverNames <- hello.ServerName
			return nil, nil
		},
	}
	server.StartTLS()
	defer server.Close()

	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(rootCertificate)
	backend := domain.Backend{
		ProxyID: 1,
		BaseURL: "https://upstream.example",
		Proxy: &domain.ProxyServer{
			Protocol: domain.ProxyProtocolTransparent,
			Address:  strings.TrimPrefix(server.URL, "https://"),
			Enabled:  true,
		},
	}
	client, err := NewHTTPClientForBackend(backend, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("create transparent proxy client: %v", err)
	}
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: rootCAs}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, backend.BaseURL+"/v1/models", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request through TCP forwarder: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("response status=%d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := <-serverNames; got != "upstream.example" {
		t.Fatalf("TLS SNI = %q, want upstream.example", got)
	}
	if got := <-serverNames; got != "upstream.example|upstream.example" {
		t.Fatalf("request TLS name and Host = %q", got)
	}
}

func TestValidateTransparentProxyRejectsAuthentication(t *testing.T) {
	server := domain.ProxyServer{
		Protocol: domain.ProxyProtocolTransparent,
		Address:  "127.0.0.1:443",
		Username: "user",
		Enabled:  true,
	}
	if err := ValidateServer(server); err == nil {
		t.Fatal("expected transparent TCP authentication to be rejected")
	}
}
