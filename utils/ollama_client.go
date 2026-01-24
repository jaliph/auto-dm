package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// OllamaRequest represents the request structure for Ollama API
type OllamaRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// OllamaResponse represents the response structure from Ollama API
type OllamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

// OllamaClient handles communication with Ollama server
type OllamaClient struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GenerateResponse sends a prompt to Ollama and returns the AI response
func (oc *OllamaClient) GenerateResponse(prompt string) (string, error) {
	if oc.baseURL == "" || oc.model == "" {
		return "", fmt.Errorf("Ollama not configured - URL: %s, Model: %s", oc.baseURL, oc.model)
	}

	// Create request payload
	request := OllamaRequest{
		Model:  oc.model,
		Prompt: prompt,
		Stream: false,
	}

	// Marshal request to JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	// Create HTTP request with context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/generate", oc.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	log.Printf("DEBUG: Sending request to Ollama: %s", url)
	resp, err := oc.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("Ollama request timed out after 30s")
		}
		return "", fmt.Errorf("failed to send request to Ollama: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var ollamaResp OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %v", err)
	}

	log.Printf("DEBUG: Received response from Ollama model %s", ollamaResp.Model)
	return ollamaResp.Response, nil
}

// IsConfigured checks if Ollama is properly configured
func (oc *OllamaClient) IsConfigured() bool {
	return oc.baseURL != "" && oc.model != ""
}

// TestConnection tests the connection to Ollama server with a short timeout
func (oc *OllamaClient) TestConnection() error {
	if !oc.IsConfigured() {
		return fmt.Errorf("Ollama not configured")
	}

	// Create a short timeout client for testing (5 seconds)
	testClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Just check if the server is reachable
	url := fmt.Sprintf("%s/api/tags", oc.baseURL)
	resp, err := testClient.Get(url)
	if err != nil {
		return fmt.Errorf("Ollama server unreachable: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama server returned status %d", resp.StatusCode)
	}

	return nil
}
