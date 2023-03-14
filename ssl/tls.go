package ssl

import (
	"crypto/tls"
	"fmt"
)

func LoadCert(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("Failed to load certificate: %w", err)
	}
	return cert, nil
}