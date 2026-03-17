package captcha

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	twocaptchaDefaultEndpoint = "https://2captcha.com"
	twocaptchaPollInterval    = 5 * time.Second
)

// TwoCaptcha implements Solver using the 2captcha API.
type TwoCaptcha struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewTwoCaptcha creates a TwoCaptcha that calls the production 2captcha API.
func NewTwoCaptcha(apiKey string) *TwoCaptcha {
	return &TwoCaptcha{
		apiKey:   apiKey,
		endpoint: twocaptchaDefaultEndpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// NewTwoCaptchaWithEndpoint creates a TwoCaptcha with a custom base URL.
// This is used in tests to point at a mock server.
func NewTwoCaptchaWithEndpoint(apiKey, endpoint string) *TwoCaptcha {
	return &TwoCaptcha{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// twocaptchaMethod maps a CaptchaType to the 2captcha method parameter.
func twocaptchaMethod(ct CaptchaType) string {
	switch ct {
	case CaptchaRecaptcha:
		return "userrecaptcha"
	case CaptchaHCaptcha:
		return "hcaptcha"
	case CaptchaCloudflare:
		return "turnstile"
	default:
		return "userrecaptcha"
	}
}

// Solve submits the challenge to 2captcha and polls until resolved.
func (s *TwoCaptcha) Solve(ctx context.Context, challenge *DetectResult) (*Solution, error) {
	captchaID, err := s.submitTask(ctx, challenge)
	if err != nil {
		return nil, fmt.Errorf("2captcha: submit task: %w", err)
	}

	slog.Debug("2captcha: task submitted", "captcha_id", captchaID, "type", challenge.Type)

	token, err := s.pollResult(ctx, captchaID)
	if err != nil {
		return nil, fmt.Errorf("2captcha: poll result: %w", err)
	}

	return &Solution{Token: token, Type: challenge.Type}, nil
}

// submitTask posts to /in.php and returns the captcha ID.
func (s *TwoCaptcha) submitTask(ctx context.Context, challenge *DetectResult) (string, error) {
	params := url.Values{}
	params.Set("key", s.apiKey)
	params.Set("method", twocaptchaMethod(challenge.Type))
	params.Set("googlekey", challenge.SiteKey)
	params.Set("pageurl", challenge.PageURL)
	params.Set("json", "0")

	reqURL := s.endpoint + "/in.php?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	text := strings.TrimSpace(string(body))
	if !strings.HasPrefix(text, "OK|") {
		return "", fmt.Errorf("2captcha in.php error: %s", text)
	}
	return strings.TrimPrefix(text, "OK|"), nil
}

// pollResult polls /res.php until the solution is ready.
func (s *TwoCaptcha) pollResult(ctx context.Context, captchaID string) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		params := url.Values{}
		params.Set("key", s.apiKey)
		params.Set("action", "get")
		params.Set("id", captchaID)
		params.Set("json", "0")

		reqURL := s.endpoint + "/res.php?" + params.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", err
		}

		resp, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		text := strings.TrimSpace(string(body))
		if text == "CAPCHA_NOT_READY" || text == "CAPTCHA_NOT_READY" {
			slog.Debug("2captcha: waiting for solution", "captcha_id", captchaID)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(twocaptchaPollInterval):
			}
			continue
		}
		if strings.HasPrefix(text, "OK|") {
			return strings.TrimPrefix(text, "OK|"), nil
		}
		return "", fmt.Errorf("2captcha res.php error: %s", text)
	}
}

// Balance returns the 2captcha account balance in USD.
func (s *TwoCaptcha) Balance(ctx context.Context) (float64, error) {
	params := url.Values{}
	params.Set("key", s.apiKey)
	params.Set("action", "getbalance")
	params.Set("json", "0")

	reqURL := s.endpoint + "/res.php?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("2captcha: balance request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	text := strings.TrimSpace(string(body))
	if strings.HasPrefix(text, "OK|") {
		text = strings.TrimPrefix(text, "OK|")
	}

	bal, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("2captcha: parsing balance %q: %w", text, err)
	}
	return bal, nil
}
