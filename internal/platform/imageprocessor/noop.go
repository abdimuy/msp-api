package imageprocessor

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

// NoOpProcessor is a passthrough [Processor]. It reads in.Body fully and
// returns the bytes unchanged. Used by tests and when an operator opts
// out at runtime via IMAGEPROCESSOR_ENABLED=false.
type NoOpProcessor struct{}

// Process buffers in.Body and returns it verbatim. ContentType is
// propagated as-is — NoOp does not sniff or validate the MIME.
func (NoOpProcessor) Process(_ context.Context, in Input) (Output, error) {
	body, err := io.ReadAll(in.Body)
	if err != nil {
		return Output{}, fmt.Errorf("imageprocessor.noop: read: %w", err)
	}
	return Output{
		Body:        bytes.NewReader(body),
		ContentType: in.ContentType,
		SizeBytes:   int64(len(body)),
	}, nil
}

// Compile-time assertion that NoOpProcessor implements Processor.
var _ Processor = NoOpProcessor{}
