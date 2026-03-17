package engine

import (
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// StepAction identifies what a Trail Step should do.
type StepAction int

const (
	// StepNavigate navigates to a URL (creates a Job).
	StepNavigate StepAction = iota
	// StepClick clicks a CSS selector (browser-mode only).
	StepClick
	// StepWait waits for a CSS selector to appear or a fixed duration.
	StepWait
	// StepExtract runs a Processor against the current page.
	StepExtract
	// StepScroll scrolls the page (browser-mode only).
	StepScroll
)

// Step is a single action within a Trail.
type Step struct {
	// Action is the kind of step.
	Action StepAction
	// URL is the target for StepNavigate.
	URL string
	// Selector is the CSS selector for StepClick, StepWait, and StepExtract.
	Selector string
	// Duration is the timeout/wait for StepWait.
	Duration time.Duration
	// Process is the extraction logic for StepExtract.
	Process foxhound.Processor
}

// Trail is a reusable navigation blueprint composed of ordered Steps.
// It is built via a fluent builder API and converted to Jobs when submitted to
// a Hunt.
type Trail struct {
	// Name is a human-readable label for this navigation path.
	Name  string
	// Steps is the ordered sequence of actions.
	Steps []Step
}

// NewTrail creates a new empty Trail with the given name.
func NewTrail(name string) *Trail {
	return &Trail{Name: name}
}

// Navigate appends a StepNavigate step that fetches url.
func (t *Trail) Navigate(url string) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepNavigate, URL: url})
	return t
}

// Click appends a StepClick step that clicks the element matching selector.
// This step is only meaningful when using the browser fetcher.
func (t *Trail) Click(selector string) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepClick, Selector: selector})
	return t
}

// Wait appends a StepWait step that blocks until selector appears or timeout
// elapses.
func (t *Trail) Wait(selector string, timeout time.Duration) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepWait, Selector: selector, Duration: timeout})
	return t
}

// Extract appends a StepExtract step that runs processor against the current
// page response.
func (t *Trail) Extract(processor foxhound.Processor) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepExtract, Process: processor})
	return t
}

// Scroll appends a StepScroll step that scrolls the page. This step is only
// meaningful when using the browser fetcher.
func (t *Trail) Scroll() *Trail {
	t.Steps = append(t.Steps, Step{Action: StepScroll})
	return t
}

// ToJobs converts every StepNavigate in the Trail into a foxhound.Job.
// Non-navigate steps are silently skipped because they have no URL target.
func (t *Trail) ToJobs() []*foxhound.Job {
	var jobs []*foxhound.Job
	for _, step := range t.Steps {
		if step.Action != StepNavigate {
			continue
		}
		jobs = append(jobs, &foxhound.Job{
			ID:        step.URL,
			URL:       step.URL,
			Method:    "GET",
			CreatedAt: time.Now(),
		})
	}
	return jobs
}
