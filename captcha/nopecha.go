package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const (
	nopechaDefaultEndpoint = "https://api.nopecha.com"
	nopechaPollInterval    = 3 * time.Second
)

// NopeCHA implements Solver using the NopeCHA Token API.
// Token API is browserless: submit sitekey + URL, get a solved token back.
// Supports hCaptcha, reCAPTCHA v2/v3, and Cloudflare Turnstile.
type NopeCHA struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewNopeCHA creates a NopeCHA solver that calls the production API.
func NewNopeCHA(apiKey string) *NopeCHA {
	return &NopeCHA{
		apiKey:   apiKey,
		endpoint: nopechaDefaultEndpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// NewNopeCHAWithEndpoint creates a NopeCHA solver with a custom base URL (for tests).
func NewNopeCHAWithEndpoint(apiKey, endpoint string) *NopeCHA {
	return &NopeCHA{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// nopechaTokenType maps CaptchaType to NopeCHA Token API type strings.
func nopechaTokenType(ct CaptchaType) string {
	switch ct {
	case CaptchaCloudflare:
		return "turnstile"
	case CaptchaRecaptcha:
		return "recaptcha2"
	case CaptchaHCaptcha:
		return "hcaptcha"
	default:
		return "recaptcha2"
	}
}

// nopechaSubmitRequest is the JSON body for submitting a token job.
type nopechaSubmitRequest struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	SiteKey string `json:"sitekey"`
	URL     string `json:"url"`
}

// nopechaResponse is the generic NopeCHA API response.
type nopechaResponse struct {
	Data    any    `json:"data"`    // string: job ID on submit, token on poll
	Error   int    `json:"error"`   // error code (0 = none)
	Message string `json:"message"` // error message
}

// nopechaStatusResponse is the response from /v1/status.
type nopechaStatusResponse struct {
	Plan   string  `json:"plan"`
	Credit float64 `json:"credit"`
	Quota  float64 `json:"quota"`
}

// Solve submits a token challenge and polls until resolved.
func (s *NopeCHA) Solve(ctx context.Context, challenge *DetectResult) (*Solution, error) {
	jobID, err := s.submit(ctx, challenge)
	if err != nil {
		return nil, fmt.Errorf("nopecha: submit: %w", err)
	}
	slog.Debug("nopecha: job submitted", "job_id", jobID, "type", challenge.Type)

	token, err := s.poll(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("nopecha: poll: %w", err)
	}
	return &Solution{Token: token, Type: challenge.Type}, nil
}

// submit posts a token job and returns the job ID.
func (s *NopeCHA) submit(ctx context.Context, challenge *DetectResult) (string, error) {
	body := nopechaSubmitRequest{
		Key:     s.apiKey,
		Type:    nopechaTokenType(challenge.Type),
		SiteKey: challenge.SiteKey,
		URL:     challenge.PageURL,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.endpoint+"/token/", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result nopechaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if result.Error != 0 {
		return "", fmt.Errorf("API error %d: %s", result.Error, result.Message)
	}
	jobID, ok := result.Data.(string)
	if !ok || jobID == "" {
		return "", fmt.Errorf("unexpected data field: %v", result.Data)
	}
	return jobID, nil
}

// poll queries for the job result until complete or context is cancelled.
func (s *NopeCHA) poll(ctx context.Context, jobID string) (string, error) {
	params := url.Values{}
	params.Set("key", s.apiKey)
	params.Set("id", jobID)
	pollURL := s.endpoint + "/token/?" + params.Encode()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return "", err
		}

		resp, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		var result nopechaResponse
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return "", fmt.Errorf("decoding poll response: %w", err)
		}

		// 409 with error 14 = "Incomplete job" — keep polling.
		if resp.StatusCode == http.StatusConflict && result.Error == 14 {
			slog.Debug("nopecha: waiting for solution", "job_id", jobID)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(nopechaPollInterval):
			}
			continue
		}

		if result.Error != 0 {
			return "", fmt.Errorf("API error %d: %s", result.Error, result.Message)
		}

		token, ok := result.Data.(string)
		if !ok || token == "" {
			return "", fmt.Errorf("unexpected token data: %v", result.Data)
		}
		return token, nil
	}
}

// Balance returns the remaining credits from the NopeCHA account.
func (s *NopeCHA) Balance(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.endpoint+"/v1/status", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Basic "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("nopecha: status request: %w", err)
	}
	defer resp.Body.Close()

	var result nopechaStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("nopecha: decoding status: %w", err)
	}
	return result.Credit, nil
}
