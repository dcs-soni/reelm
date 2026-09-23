package proxy

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CAManager generates and caches on-the-fly TLS certificates signed by a local Root CA.
type CAManager struct {
	caCert    *x509.Certificate
	caKey     *rsa.PrivateKey
	certCache sync.Map // host -> *tls.Certificate
}

// NewCAManager loads or creates a CA certificate and private key in the specified directory.
func NewCAManager(caDir string) (*CAManager, error) {
	if caDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			caDir = "./.reelm/ca"
		} else {
			caDir = filepath.Join(home, ".reelm", "ca")
		}
	}

	if err := os.MkdirAll(caDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create CA directory: %w", err)
	}

	certFile := filepath.Join(caDir, "reelm-ca.crt")
	keyFile := filepath.Join(caDir, "reelm-ca.key")

	var caCert *x509.Certificate
	var caKey *rsa.PrivateKey

	// Check if existing CA cert and key exist
	if _, errC := os.Stat(certFile); errC == nil {
		if _, errK := os.Stat(keyFile); errK == nil {
			caCert, caKey, errC = loadCA(certFile, keyFile)
			if errC == nil {
				return &CAManager{caCert: caCert, caKey: caKey}, nil
			}
		}
	}

	// Generate new self-signed CA
	caCert, caKey, err := generateCA(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Root CA: %w", err)
	}

	return &CAManager{caCert: caCert, caKey: caKey}, nil
}

func generateCA(certFile, keyFile string) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "Reelm Local Proxy Root CA",
			Organization: []string{"Reelm Development CA"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := os.WriteFile(certFile, certPem, 0644); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(keyFile, keyPem, 0600); err != nil {
		return nil, nil, err
	}

	parsedCert, err := x509.ParseCertificate(derBytes)
	return parsedCert, key, err
}

func loadCA(certFile, keyFile string) (*x509.Certificate, *rsa.PrivateKey, error) {
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return nil, nil, err
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, nil, err
	}

	certBlock, _ := pem.Decode(certData)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("invalid PEM in cert file")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("invalid PEM in key file")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

// GetHostCertificate returns a cached or dynamically generated TLS certificate for host.
func (m *CAManager) GetHostCertificate(host string) (*tls.Certificate, error) {
	// Strip port if present
	if strings.Contains(host, ":") {
		h, _, err := net.SplitHostPort(host)
		if err == nil {
			host = h
		}
	}

	if val, ok := m.certCache.Load(host); ok {
		return val.(*tls.Certificate), nil
	}

	// Generate certificate for this specific host
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: host,
		},
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &priv.PublicKey, m.caKey)
	if err != nil {
		return nil, err
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{derBytes, m.caCert.Raw},
		PrivateKey:  priv,
	}

	m.certCache.Store(host, tlsCert)
	return tlsCert, nil
}

// HandleConnect hijacks an HTTP CONNECT request to perform transparent TLS interception.
func (h *Handler) HandleConnect(w http.ResponseWriter, r *http.Request) {
	if h.caManager == nil {
		h.logger.Error().Msg("TLS MITM requested but CA manager is not initialized")
		http.Error(w, "TLS MITM not enabled", http.StatusBadGateway)
		return
	}

	targetHost := r.Host

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		h.logger.Error().Msg("webserver does not support hijacking for CONNECT")
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to hijack client connection")
		return
	}
	defer clientConn.Close()

	// Acknowledge connection established
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Create TLS server configuration with on-the-fly certificate generation
	tlsConfig := &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			serverName := hello.ServerName
			if serverName == "" {
				serverName = targetHost
			}
			return h.caManager.GetHostCertificate(serverName)
		},
	}

	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		h.logger.Debug().Err(err).Str("host", targetHost).Msg("TLS handshake with client failed")
		return
	}
	defer tlsConn.Close()

	// Read decrypted HTTP requests from the TLS connection and dispatch through Handler
	bufReader := bufio.NewReader(tlsConn)
	for {
		innerReq, err := http.ReadRequest(bufReader)
		if err != nil {
			break // connection closed or EOF
		}

		// Fix request URL and scheme since it was read from wire
		innerReq.URL.Scheme = "https"
		if innerReq.URL.Host == "" {
			innerReq.URL.Host = targetHost
		}
		innerReq.Host = targetHost

		respWriter := newConnResponseWriter(tlsConn)
		h.ServeHTTP(respWriter, innerReq)
		_ = respWriter.flush()
	}
}

// connResponseWriter writes standard HTTP/1.1 responses over a net.Conn.
type connResponseWriter struct {
	conn       net.Conn
	header     http.Header
	statusCode int
	wroteHead  bool
}

func newConnResponseWriter(conn net.Conn) *connResponseWriter {
	return &connResponseWriter{
		conn:       conn,
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (w *connResponseWriter) Header() http.Header {
	return w.header
}

func (w *connResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHead {
		w.statusCode = statusCode
		w.wroteHead = true
		fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\n", statusCode, http.StatusText(statusCode))
		_ = w.header.Write(w.conn)
		fmt.Fprintf(w.conn, "\r\n")
	}
}

func (w *connResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHead {
		w.WriteHeader(http.StatusOK)
	}
	return w.conn.Write(p)
}

func (w *connResponseWriter) Flush() {
	// TCP socket writes are directly flushed
}

func (w *connResponseWriter) flush() error {
	if !w.wroteHead {
		w.WriteHeader(http.StatusOK)
	}
	return nil
}
