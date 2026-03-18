package pipeline

import (
	"context"

	foxhound "github.com/sadewadee/foxhound"
)

// Chain combines multiple Pipeline stages into a single Pipeline.
// Stages are executed in the order they were provided to NewChain.
// If any stage returns nil (drop), the item is dropped and subsequent
// stages are skipped. If any stage returns an error, processing stops
// and the error is returned immediately.
type Chain struct {
	stages []foxhound.Pipeline
}

// NewChain returns a Chain that runs the given stages in order.
func NewChain(stages ...foxhound.Pipeline) *Chain {
	return &Chain{stages: stages}
}

// Process runs item through each stage in order. Returns nil if any stage
// drops the item, or an error if any stage fails.
func (c *Chain) Process(ctx context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	current := item
	for _, stage := range c.stages {
		var err error
		current, err = stage.Process(ctx, current)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, nil
		}
	}
	return current, nil
}
