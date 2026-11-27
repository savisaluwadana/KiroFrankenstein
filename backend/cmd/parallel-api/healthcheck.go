package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

// runHealthcheck implements `parallel-api healthcheck` for container
// HEALTHCHECKs (distroless images have no curl). It GETs
// http://127.0.0.1:$PORT/healthz (PORT defaults to 8080) and fails unless the
// answer is 200.
func runHealthcheck(ctx context.Context) error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthz unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
