package captcha

import "context"

// Solution contains a solved CAPTCHA token ready to be submitted to the target site.
type Solution struct {
	// Token is the solved CAPTCHA response string.
	Token string
	// Type is the CAPTCHA type that was solved.
	Type CaptchaType
}

// Solver solves CAPTCHA challenges using an external service.
type Solver interface {
	// Solve submits the challenge to the solving service and returns the token.
	Solve(ctx context.Context, challenge *DetectResult) (*Solution, error)
	// Balance returns the solver account balance in USD.
	Balance(ctx context.Context) (float64, error)
}
