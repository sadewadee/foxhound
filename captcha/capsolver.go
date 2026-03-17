package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	capsolverDefaultEndpoint = "https://api.capsolver.com"
	capsolverPollInterval    = 3 * time.Second
)

// CapSolver implements Solver using the CapSolver API.
type CapSolver struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewCapSolver creates a CapSolver that calls the production CapSolver API.
func NewCapSolver(apiKey string) *CapSolver {
	return &CapSolver{
		apiKey:   apiKey,
		endpoint: capsolverDefaultEndpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// NewCapSolverWithEndpoint creates a CapSolver with a custom base URL.
// This is used in tests to point at a mock server.
func NewCapSolverWithEndpoint(apiKey, endpoint string) *CapSolver {
	return &CapSolver{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// capsolverTaskType maps a CaptchaType to the CapSolver task type string.
func capsolverTaskType(ct CaptchaType) string {
	switch ct {
	case CaptchaCloudflare:
		return "AntiTurnstileTaskProxyLess"
	case CaptchaRecaptcha:
		return "ReCaptchaV2TaskProxyLess"
	case CaptchaHCaptcha:
		return "HCaptchaTaskProxyLess"
	default:
		return "ReCaptchaV2TaskProxyLess"
	}
}

// capsolverCreateTaskRequest is the JSON body for the createTask endpoint.
type capsolverCreateTaskRequest struct {
	ClientKey string         `json:"clientKey"`
	Task      map[string]any `json:"task"`
}

// capsolverCreateTaskResponse is the response from createTask.
type capsolverCreateTaskResponse struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode"`
	ErrorDescription string `json:"errorDescription"`
	TaskID           string `json:"taskId"`
}

// capsolverGetResultRequest is the JSON body for getTaskResult.
type capsolverGetResultRequest struct {
	ClientKey string `json:"clientKey"`
	TaskID    string `json:"taskId"`
}

// capsolverGetResultResponse is the response from getTaskResult.
type capsolverGetResultResponse struct {
	ErrorID          int            `json:"errorId"`
	ErrorCode        string         `json:"errorCode"`
	ErrorDescription string         `json:"errorDescription"`
	Status           string         `json:"status"`
	Solution         map[string]any `json:"solution"`
}

// capsolverBalanceResponse is the response from getBalance.
type capsolverBalanceResponse struct {
	ErrorID int     `json:"errorId"`
	Balance float64 `json:"balance"`
}

// Solve submits the challenge to CapSolver and polls until resolved.
func (s *CapSolver) Solve(ctx context.Context, challenge *DetectResult) (*Solution, error) {
	taskID, err := s.createTask(ctx, challenge)
	if err != nil {
		return nil, fmt.Errorf("capsolver: create task: %w", err)
	}

	slog.Debug("capsolver: task created", "task_id", taskID, "type", challenge.Type)

	token, err := s.pollResult(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("capsolver: poll result: %w", err)
	}

	return &Solution{Token: token, Type: challenge.Type}, nil
}

// createTask submits a new solving task and returns the task ID.
func (s *CapSolver) createTask(ctx context.Context, challenge *DetectResult) (string, error) {
	body := capsolverCreateTaskRequest{
		ClientKey: s.apiKey,
		Task: map[string]any{
			"type":       capsolverTaskType(challenge.Type),
			"websiteURL": challenge.PageURL,
			"websiteKey": challenge.SiteKey,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.endpoint+"/createTask", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result capsolverCreateTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if result.ErrorID != 0 {
		return "", fmt.Errorf("API error %s: %s", result.ErrorCode, result.ErrorDescription)
	}
	return result.TaskID, nil
}

// pollResult polls getTaskResult until the task is complete or context is done.
func (s *CapSolver) pollResult(ctx context.Context, taskID string) (string, error) {
	body := capsolverGetResultRequest{
		ClientKey: s.apiKey,
		TaskID:    taskID,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.endpoint+"/getTaskResult", bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		var result capsolverGetResultResponse
		decErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if decErr != nil {
			return "", fmt.Errorf("decoding result: %w", decErr)
		}
		if result.ErrorID != 0 {
			return "", fmt.Errorf("API error %s: %s", result.ErrorCode, result.ErrorDescription)
		}
		if result.Status == "ready" {
			return extractCapsolverToken(result.Solution), nil
		}

		slog.Debug("capsolver: waiting for solution", "task_id", taskID, "status", result.Status)

		// Re-encode for the next iteration.
		data, err = json.Marshal(body)
		if err != nil {
			return "", err
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(capsolverPollInterval):
		}
	}
}

// extractCapsolverToken pulls the token from the solution map.
// Different task types use different field names.
func extractCapsolverToken(solution map[string]any) string {
	for _, key := range []string{"token", "gRecaptchaResponse", "captchaAnswer"} {
		if v, ok := solution[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// Balance returns the CapSolver account balance in USD.
func (s *CapSolver) Balance(ctx context.Context) (float64, error) {
	body := map[string]string{"clientKey": s.apiKey}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.endpoint+"/getBalance", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("capsolver: balance request: %w", err)
	}
	defer resp.Body.Close()

	var result capsolverBalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("capsolver: decoding balance response: %w", err)
	}
	return result.Balance, nil
}
