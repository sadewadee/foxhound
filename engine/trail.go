package engine

import (
	"fmt"
	"time"

	foxhound "github.com/sadewadee/foxhound"
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
	// StepInfiniteScroll scrolls to bottom repeatedly until no new content
	// loads (for lazy-load / infinite scroll pages like Google Maps).
	StepInfiniteScroll
	// StepLoadMore clicks a "load more" button repeatedly until it
	// disappears or max clicks reached.
	StepLoadMore
	// StepPaginate detects pagination links ("Next", page numbers) and
	// follows them, collecting content from each page.
	StepPaginate
	// StepEvaluate executes custom JavaScript on the page.
	StepEvaluate
	// StepFill types text into an input field with human-like keystrokes.
	StepFill
	// StepCollect extracts URLs from matching elements into a Pool.
	StepCollect
)

// Step is a single action within a Trail.
type Step struct {
	// Action is the kind of step.
	Action StepAction
	// URL is the target for StepNavigate.
	URL string
	// Selector is the CSS selector for StepClick, StepWait, and StepExtract.
	// For InfiniteScroll, Selector is the scrollable container (empty = whole page).
	Selector string
	// Duration is the timeout/wait for StepWait.
	Duration time.Duration
	// Process is the extraction logic for StepExtract.
	Process foxhound.Processor
	// MaxScrolls limits InfiniteScroll iterations.
	MaxScrolls int
	// MaxClicks limits LoadMore button clicks.
	MaxClicks int
	// MaxPages limits Paginate page follows.
	MaxPages int
	// Script is the JavaScript code for StepEvaluate.
	Script string
	// StopSelector is a CSS selector; InfiniteScroll stops when
	// document.querySelectorAll(StopSelector).length >= StopCount.
	StopSelector string
	// StopCount is the target element count for StopSelector.
	StopCount int
	// ScrollWait is the duration to wait after each scroll iteration before
	// checking for new content. Defaults to 2s when zero.
	ScrollWait time.Duration
	// Optional marks this step as non-fatal: if it fails, execution continues.
	Optional bool
	// Value is the text to type into an input field for StepFill.
	Value string
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

// InfiniteScroll appends a step that scrolls to the bottom repeatedly until
// no new content loads (for lazy-load / infinite scroll pages). maxScrolls
// limits iterations (0 = default 50). Scrolls the whole page.
func (t *Trail) InfiniteScroll(maxScrolls int) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepInfiniteScroll, MaxScrolls: maxScrolls})
	return t
}

// InfiniteScrollWithWait appends an InfiniteScroll with custom post-scroll wait.
func (t *Trail) InfiniteScrollWithWait(maxScrolls int, scrollWait time.Duration) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepInfiniteScroll, MaxScrolls: maxScrolls, ScrollWait: scrollWait})
	return t
}

// InfiniteScrollIn appends an InfiniteScroll step that scrolls inside a
// specific container element (e.g. Google Maps results panel, Facebook feed).
// container is a CSS selector for the scrollable element.
func (t *Trail) InfiniteScrollIn(container string, maxScrolls int) *Trail {
	t.Steps = append(t.Steps, Step{
		Action:     StepInfiniteScroll,
		Selector:   container,
		MaxScrolls: maxScrolls,
	})
	return t
}

// InfiniteScrollUntil appends an InfiniteScroll step that stops when
// stopSelector matches at least stopCount elements. This scrolls until
// the target is reached rather than until content stops loading.
func (t *Trail) InfiniteScrollUntil(stopSelector string, stopCount int, maxScrolls int) *Trail {
	t.Steps = append(t.Steps, Step{
		Action:       StepInfiniteScroll,
		MaxScrolls:   maxScrolls,
		StopSelector: stopSelector,
		StopCount:    stopCount,
	})
	return t
}

// InfiniteScrollInUntil combines container scrolling with a stop condition.
func (t *Trail) InfiniteScrollInUntil(container, stopSelector string, stopCount, maxScrolls int) *Trail {
	t.Steps = append(t.Steps, Step{
		Action:       StepInfiniteScroll,
		Selector:     container,
		MaxScrolls:   maxScrolls,
		StopSelector: stopSelector,
		StopCount:    stopCount,
	})
	return t
}

// Evaluate appends a step that executes custom JavaScript on the page.
// The return value of the script is available in Response.StepResults.
func (t *Trail) Evaluate(script string) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepEvaluate, Script: script})
	return t
}

// LoadMore appends a step that clicks the element matching selector repeatedly
// until it disappears or maxClicks is reached (0 = default 20).
func (t *Trail) LoadMore(selector string, maxClicks int) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepLoadMore, Selector: selector, MaxClicks: maxClicks})
	return t
}

// Paginate appends a step that detects pagination links matching selector
// (e.g. "a.next", "li.next a") and follows them, collecting content from each
// page. maxPages limits how many pages to follow (0 = default 10).
func (t *Trail) Paginate(selector string, maxPages int) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepPaginate, Selector: selector, MaxPages: maxPages})
	return t
}

// ClickOptional appends a StepClick step that does NOT abort the fetch on
// failure. Useful for dismissing elements that may or may not be present.
func (t *Trail) ClickOptional(selector string) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepClick, Selector: selector, Optional: true})
	return t
}

// WaitOptional appends a StepWait step that does NOT abort the fetch on
// failure. Useful for waiting on elements that may not appear on every page.
func (t *Trail) WaitOptional(selector string, timeout time.Duration) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepWait, Selector: selector, Duration: timeout, Optional: true})
	return t
}

// Fill appends a StepFill step that types value into the input matching
// selector with human-like keystrokes (using behavior.Keyboard).
func (t *Trail) Fill(selector, value string) *Trail {
	t.Steps = append(t.Steps, Step{Action: StepFill, Selector: selector, Value: value})
	return t
}

// Collect appends a step that extracts URLs from all elements matching
// selector, reading the given attribute (typically "href"). The collected
// URLs are stored in Response.StepResults as []string.
//
// This step is implemented as a JS Evaluate that runs
// querySelectorAll(selector) and returns the attribute values.
func (t *Trail) Collect(selector, attr string) *Trail {
	t.Steps = append(t.Steps, Step{
		Action:   StepCollect,
		Selector: selector,
		Value:    attr,
	})
	return t
}

// ToJobs converts the Trail into foxhound.Jobs. Each StepNavigate starts a
// new Job; subsequent browser steps (Click, Wait, Scroll) are attached as
// JobSteps on that Job and set FetchMode to FetchBrowser.
//
// Extract steps are NOT converted to JobSteps because their Processor
// (an interface) cannot survive JSON serialization through queue backends.
// Extraction is handled by the hunt-level Processor after the fetch completes.
//
// Steps that appear before the first Navigate are silently skipped.
func (t *Trail) ToJobs() []*foxhound.Job {
	var jobs []*foxhound.Job
	var current *foxhound.Job

	for _, step := range t.Steps {
		if step.Action == StepNavigate {
			// Start a new segment.
			current = &foxhound.Job{
				ID:        step.URL,
				URL:       step.URL,
				Method:    "GET",
				CreatedAt: time.Now(),
			}
			jobs = append(jobs, current)
			continue
		}

		// Non-navigate steps attach to the current job.
		if current == nil {
			continue // no navigate yet — skip orphaned step
		}

		// Extract steps cannot be serialized — skip them.
		if step.Action == StepExtract {
			continue
		}

		// Collect steps are implemented as JS Evaluate steps.
		if step.Action == StepCollect {
			script := fmt.Sprintf(
				`() => [...document.querySelectorAll('%s')].map(el => el.getAttribute('%s')).filter(Boolean)`,
				step.Selector, step.Value,
			)
			js := foxhound.JobStep{
				Action:   foxhound.JobStepEvaluate,
				Script:   script,
				Selector: step.Selector,
				Value:    step.Value,
			}
			current.Steps = append(current.Steps, js)
			current.FetchMode = foxhound.FetchBrowser
			continue
		}

		js := foxhound.JobStep{
			Action:       mapStepAction(step.Action),
			Selector:     step.Selector,
			Duration:     step.Duration,
			MaxScrolls:   step.MaxScrolls,
			MaxClicks:    step.MaxClicks,
			MaxPages:     step.MaxPages,
			Script:       step.Script,
			StopSelector: step.StopSelector,
			StopCount:    step.StopCount,
			ScrollWait:   step.ScrollWait,
			Optional:     step.Optional,
			Value:        step.Value,
		}
		current.Steps = append(current.Steps, js)

		// Browser-only steps force FetchBrowser.
		current.FetchMode = foxhound.FetchBrowser
	}
	return jobs
}

// mapStepAction converts an engine.StepAction to the package-level JobStep*
// constant defined in foxhound.go. Only browser-executable actions (Click,
// Wait, Scroll) are mapped; Extract is skipped before reaching this function.
func mapStepAction(a StepAction) int {
	switch a {
	case StepClick:
		return foxhound.JobStepClick
	case StepWait:
		return foxhound.JobStepWait
	case StepScroll:
		return foxhound.JobStepScroll
	case StepInfiniteScroll:
		return foxhound.JobStepInfiniteScroll
	case StepLoadMore:
		return foxhound.JobStepLoadMore
	case StepPaginate:
		return foxhound.JobStepPaginate
	case StepEvaluate:
		return foxhound.JobStepEvaluate
	case StepFill:
		return foxhound.JobStepFill
	case StepCollect:
		return foxhound.JobStepEvaluate
	default:
		return foxhound.JobStepNavigate
	}
}
