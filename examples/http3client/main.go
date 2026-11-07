// nolint
//
//nolint:nolintlint
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/quic-go/quic-go/http3"
)

const (
	// TLS configuration
	enableMTLS = true // false для отключения проверки TLS (insecure)

	// Certificate paths
	clientCertFile = "examples/local_crt/client-cert.pem"
	clientKeyFile  = "examples/local_crt/client-key.pem"
	caCertFile     = "examples/local_crt/ca-cert.pem"
)

func main() {
	tlsConfig := createTLSConfig()

	// Создаем HTTP/3 клиент
	roundTripper := &http3.Transport{
		TLSClientConfig: tlsConfig,
	}
	defer roundTripper.Close() //nolint:errcheck

	client := &http.Client{
		Transport: roundTripper,
	}

	performRequests(client)

	fmt.Println("\nHTTP/3 client completed successfully")
}

func createTLSConfig() *tls.Config {
	if !enableMTLS {
		log.Println("TLS verification disabled (insecure mode)")
		return &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	cert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		log.Fatalf("Failed to load client certificate: %v", err)
	}

	caCertPEM, err := os.ReadFile(caCertFile)
	if err != nil {
		log.Fatalf("Failed to read CA certificate: %v", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertPEM) {
		log.Fatal("Failed to parse CA certificate")
	}

	log.Println("Mutual TLS enabled")

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
}

func performRequests(client *http.Client) {
	serverURL := "https://localhost:8080"

	fmt.Println("==> Requesting /health")
	if err := makeRequest(client, serverURL+"/health"); err != nil {
		log.Printf("Error requesting /health: %v\n", err)
	}

	fmt.Println("\n==> Requesting /api/users/12345")
	if err := makeRequest(client, serverURL+"/api/users/12345"); err != nil {
		log.Printf("Error requesting /api/users/12345: %v\n", err)
	}
}

func makeRequest(client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Proto: %s\n", resp.Proto)
	fmt.Printf("Body: %s\n", string(body))

	return nil
}
