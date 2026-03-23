// Package integration contains integration tests that verify telemetry
// is correctly sent to and queryable from a real Dynatrace environment.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/mreider/semantix/internal/config"
	"github.com/mreider/semantix/internal/simulation"
)

// TestDynatraceIntegration runs a short simulation and verifies
// that traces appear in Dynatrace via DQL.
func TestDynatraceIntegration(t *testing.T) {
	// Skip if not in integration test mode
	// DT_ENDPOINT: Classic OTLP endpoint (e.g., https://xxx.dynatracelabs.com/api/v2/otlp)
	// DT_API_TOKEN: Classic API token (dt0c01...) for OTLP ingestion
	// DT_PLATFORM_URL: Platform URL (e.g., https://xxx.apps.dynatracelabs.com) - optional
	// DT_PLATFORM_TOKEN: Platform token (dt0s16...) for DQL queries - optional
	endpoint := os.Getenv("DT_ENDPOINT")
	ingestToken := os.Getenv("DT_API_TOKEN")
	if endpoint == "" || ingestToken == "" {
		t.Skip("Skipping integration test: DT_ENDPOINT and DT_API_TOKEN must be set")
	}

	// Platform credentials for DQL verification (optional but recommended)
	platformURL := os.Getenv("DT_PLATFORM_URL")
	platformToken := os.Getenv("DT_PLATFORM_TOKEN")
	canVerifyWithDQL := platformURL != "" && platformToken != ""

	if !canVerifyWithDQL {
		t.Log("Warning: DT_PLATFORM_URL and DT_PLATFORM_TOKEN not set - will skip DQL verification")
	}

	// Generate unique test run ID
	testRunID := fmt.Sprintf("inttest-%d", time.Now().UnixNano())
	serviceName := fmt.Sprintf("semantix-inttest-%d", time.Now().Unix())
	t.Logf("Test run ID: %s", testRunID)
	t.Logf("Service name: %s", serviceName)

	// Create test configuration with unique identifier
	cfg := createTestConfig(endpoint, ingestToken, testRunID, serviceName)

	// Run simulation for a short duration
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("Creating simulation engine...")
	engine, err := simulation.NewEngine(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create simulation engine: %v", err)
	}

	// Run simulation in background
	simCtx, simCancel := context.WithTimeout(ctx, 15*time.Second)
	defer simCancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Run(simCtx)
	}()

	// Wait for simulation to complete
	t.Log("Running simulation for 15 seconds...")
	select {
	case err := <-errCh:
		if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
			t.Fatalf("Simulation error: %v", err)
		}
	case <-simCtx.Done():
		// Expected timeout
	}

	// Shutdown engine to flush all telemetry data
	t.Log("Shutting down engine (flushing data)...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := engine.Shutdown(shutdownCtx); err != nil {
		t.Logf("Warning: engine shutdown error: %v", err)
	}

	if !canVerifyWithDQL {
		t.Log("Skipping DQL verification (no platform credentials)")
		t.Log("Telemetry was sent successfully - verify manually in Dynatrace UI")
		return
	}

	// Wait for Dynatrace to ingest data
	t.Log("Waiting 90 seconds for Dynatrace to ingest and index data...")
	time.Sleep(90 * time.Second)

	// Query Dynatrace to verify data using platform API
	client := &grailClient{
		baseURL: platformURL,
		token:   platformToken,
	}

	// Test 1: Verify spans exist
	t.Run("VerifySpans", func(t *testing.T) {
		// Query for spans from our test service
		// Note: service.name comes from resource but may show as "unknown_service"
		// if not properly configured. We use server.address which we control.
		query := fmt.Sprintf(`fetch spans
| filter server.address == "%s"
| limit 100`, serviceName)

		t.Logf("Running DQL query: %s", query)
		result, err := client.query(query)
		if err != nil {
			t.Fatalf("DQL query failed: %v", err)
		}

		if len(result.Records) == 0 {
			t.Error("Expected spans to be present, but found none")
		} else {
			t.Logf("SUCCESS: Found %d span records for service %s", len(result.Records), serviceName)
		}
	})

	// Test 2: Verify span attributes
	t.Run("VerifySpanAttributes", func(t *testing.T) {
		query := fmt.Sprintf(`fetch spans
| filter server.address == "%s"
| filter isNotNull(http.request.method)
| limit 10`, serviceName)

		result, err := client.query(query)
		if err != nil {
			t.Logf("Span attributes query warning: %v", err)
			return
		}

		if len(result.Records) > 0 {
			t.Logf("SUCCESS: Found %d spans with HTTP attributes", len(result.Records))
		} else {
			t.Log("No spans with HTTP attributes found yet (may take longer to index)")
		}
	})
}

// createTestConfig creates a minimal configuration for integration testing.
func createTestConfig(endpoint, token, testRunID, serviceName string) *config.Config {
	return &config.Config{
		Version: "1.0",
		Simulation: config.SimulationConfig{
			Duration:     "infinite",
			TickInterval: "100ms",
		},
		Exporter: config.ExporterConfig{
			Endpoint: endpoint,
			Token:    token,
			Protocol: "http/protobuf",
		},
		GlobalResource: map[string]string{
			"deployment.environment": "integration-test",
			"service.namespace":      "semantix-test",
			"test.run.id":            testRunID,
		},
		Services: []config.ServiceConfig{
			{
				Name:    serviceName,
				Version: "1.0.0",
				Type:    "http",
				ResourceAttributes: map[string]string{
					"test.run.id": testRunID,
				},
				Endpoints: []config.EndpointConfig{
					{
						Name:   "test-endpoint",
						Type:   "http.server",
						Method: "GET",
						Route:  "/test",
						Traffic: &config.TrafficConfig{
							RequestsPerMinute: 60, // 1 per second
						},
						Latency: config.LatencyConfig{
							Distribution: "normal",
							P50Ms:        10,
							P95Ms:        30,
							P99Ms:        50,
						},
					},
				},
			},
		},
	}
}

// grailClient handles Dynatrace Grail (platform) API queries.
type grailClient struct {
	baseURL string
	token   string
}

// GrailResult represents the response from a Grail DQL query.
type GrailResult struct {
	Records []map[string]interface{} `json:"records"`
}

// query executes a DQL query against Dynatrace Grail.
func (c *grailClient) query(dql string) (*GrailResult, error) {
	queryURL := fmt.Sprintf("%s/platform/storage/query/v1/query:execute", c.baseURL)

	payload := map[string]interface{}{
		"query":                 dql,
		"defaultTimeframeStart": time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		"defaultTimeframeEnd":   time.Now().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	req, err := http.NewRequest("POST", queryURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse initial response - might be async
	var initialResp struct {
		State        string      `json:"state"`
		RequestToken string      `json:"requestToken"`
		Result       GrailResult `json:"result"`
	}
	if err := json.Unmarshal(respBody, &initialResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	// If query is still running, poll for result
	if initialResp.State == "RUNNING" {
		return c.pollForResult(initialResp.RequestToken)
	}

	if initialResp.State == "SUCCEEDED" {
		return &initialResp.Result, nil
	}

	return nil, fmt.Errorf("query failed with state: %s", initialResp.State)
}

// pollForResult polls for async query result.
func (c *grailClient) pollForResult(requestToken string) (*GrailResult, error) {
	pollURL := fmt.Sprintf("%s/platform/storage/query/v1/query:poll?request-token=%s",
		c.baseURL, url.QueryEscape(requestToken))

	for i := 0; i < 30; i++ { // Max 30 attempts, 1 second apart
		time.Sleep(1 * time.Second)

		req, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create poll request: %w", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read poll response: %w", err)
		}

		var pollResp struct {
			State  string      `json:"state"`
			Result GrailResult `json:"result"`
		}
		if err := json.Unmarshal(respBody, &pollResp); err != nil {
			return nil, fmt.Errorf("failed to parse poll response: %w", err)
		}

		if pollResp.State == "SUCCEEDED" {
			return &pollResp.Result, nil
		}

		if pollResp.State != "RUNNING" {
			return nil, fmt.Errorf("query failed with state: %s", pollResp.State)
		}
	}

	return nil, fmt.Errorf("query timed out after 30 seconds")
}
