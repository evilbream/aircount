package ingest

import (
	"context"
	"errors"
	"poltergeist/internal/domain"
)

type fanout []Publisher

func Fanout(publishers ...Publisher) Publisher {
	return fanout(publishers)
}

func (f fanout) PublishDevices(ctx context.Context, devices domain.DetectionBatch) error {
	var errs []error
	for _, p := range f {
		errs = append(errs, p.PublishDevices(ctx, devices))
	}
	return errors.Join(errs...)
}

func (f fanout) PublishCSI(ctx context.Context, csi domain.CSIWindow) error {
	var errs []error
	for _, p := range f {
		errs = append(errs, p.PublishCSI(ctx, csi))
	}
	return errors.Join(errs...)
}
