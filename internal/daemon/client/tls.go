package client

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// TLSMode names the four trust strategies the desktop UI offers.
// Spec §4.7.
type TLSMode string

const (
	TLSDefault    TLSMode = "default"
	TLSPinCert    TLSMode = "pin-cert"
	TLSCABundle   TLSMode = "ca-bundle"
	TLSSkipVerify TLSMode = "skip-verify"
)

// BuildTLSConfig is the canonical implementation. The desktop-remote-device
// sub-spec will copy this verbatim to the Wails frontend's Go side.
func BuildTLSConfig(mode TLSMode, certPEM string) (*tls.Config, error) {
	switch mode {
	case TLSDefault, "":
		return &tls.Config{MinVersion: tls.VersionTLS12}, nil

	case TLSPinCert:
		pinned, err := parseSingleCert(certPEM)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return errors.New("tls: no peer cert")
				}
				if !bytes.Equal(rawCerts[0], pinned.Raw) {
					return errors.New("tls: server cert does not match pinned cert")
				}
				return nil
			},
			// VerifyConnection also runs for resumed sessions, where
			// VerifyPeerCertificate is skipped. Re-pin against the leaf cert
			// here so resumption cannot bypass the manual pin.
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return errors.New("tls: no peer cert")
				}
				if !bytes.Equal(cs.PeerCertificates[0].Raw, pinned.Raw) {
					return errors.New("tls: server cert does not match pinned cert")
				}
				return nil
			},
		}, nil

	case TLSCABundle:
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(certPEM)) {
			return nil, errors.New("tls: invalid CA bundle PEM")
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}, nil

	case TLSSkipVerify:
		return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, nil

	default:
		return nil, fmt.Errorf("unknown tls mode: %q", mode)
	}
}

func parseSingleCert(pemText string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("tls: empty PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}
