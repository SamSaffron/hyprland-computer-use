package app

import (
	"context"
	"errors"
	"time"
)

const maxObservationDelayMS = 5000

// PostInputObservationOptions keeps waiting out of standalone capture, recording,
// and compositor transactions. The default remains an immediate observation.
type PostInputObservationOptions struct {
	CaptureOptions
	DelayMS int `json:"delay_ms,omitempty" jsonschema:"Wait 0–5000ms after completed input before taking the screenshot; default 0 (immediate). Gives the application time to render, not a settled-frame guarantee. Requires then=screenshot."`
}

func (o PostInputObservationOptions) validate() error {
	if o.DelayMS < 0 || o.DelayMS > maxObservationDelayMS {
		return errors.New("observation.delay_ms must be 0–5000")
	}
	return o.CaptureOptions.validate()
}

func waitForObservation(ctx context.Context, delayMS int) error {
	if delayMS == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
