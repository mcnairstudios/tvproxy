package mtls

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type EnrollResult struct {
	Cert        string `json:"cert"`
	Key         string `json:"key"`
	CA          string `json:"ca"`
	Email       string `json:"email"`
	Fingerprint string `json:"fingerprint"`
}

func Enroll(serverURL, token string) (*EnrollResult, error) {
	enrollURL := strings.TrimRight(serverURL, "/")
	if idx := strings.Index(enrollURL, "/playlist.m3u"); idx > 0 {
		enrollURL = enrollURL[:idx]
	}
	enrollURL += "/enroll"

	body, _ := json.Marshal(map[string]string{"token": token})

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Post(enrollURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("enrollment request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid or expired enrollment token")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enrollment failed with status %d", resp.StatusCode)
	}

	var result EnrollResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding enrollment response: %w", err)
	}
	return &result, nil
}

func SaveCerts(configDir, accountID string, result *EnrollResult) error {
	dir := filepath.Join(configDir, "certs", accountID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "client.crt"), []byte(result.Cert), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "client.key"), []byte(result.Key), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte(result.CA), 0600); err != nil {
		return err
	}
	return nil
}

func HasCerts(configDir, accountID string) bool {
	dir := filepath.Join(configDir, "certs", accountID)
	_, err := os.Stat(filepath.Join(dir, "client.crt"))
	return err == nil
}

func TLSClient(configDir, accountID string) (*http.Client, error) {
	dir := filepath.Join(configDir, "certs", accountID)

	cert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, "client.crt"),
		filepath.Join(dir, "client.key"),
	)
	if err != nil {
		return nil, fmt.Errorf("loading client cert: %w", err)
	}

	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("loading CA cert: %w", err)
	}

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)

	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caPool,
			},
		},
	}, nil
}

func DeleteCerts(configDir, accountID string) error {
	dir := filepath.Join(configDir, "certs", accountID)
	return os.RemoveAll(dir)
}
