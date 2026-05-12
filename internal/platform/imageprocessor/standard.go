package imageprocessor

import (
	"bytes"
	"context"
	"log/slog"
)

// StandardProcessor runs the real decode → resize → re-encode pipeline.
// Safe for concurrent use: it carries only the immutable [Options] and
// never mutates internal state.
type StandardProcessor struct {
	opts Options
}

// NewStandardProcessor returns a StandardProcessor configured with opts.
// opts.Validate is the caller's responsibility — the production wiring
// goes through [New] which validates before constructing.
func NewStandardProcessor(opts Options) *StandardProcessor {
	return &StandardProcessor{opts: opts}
}

// Process implements [Processor]. The pipeline:
//
//  1. Read up to [Options.MaxInputBytes]+1 bytes; reject oversized inputs.
//  2. Sniff the MIME from the first 512 bytes; reject unsupported types.
//  3. Optionally short-circuit when [Options.PreserveSmallImages] is true
//     and a header-only dimension probe shows the image already fits.
//  4. Decode → resize (long side capped at [Options.MaxLongSidePx]) →
//     encode using the target MIME (JPEG for WebP when
//     [Options.RecompressWebPToJPEG] is true).
func (p *StandardProcessor) Process(ctx context.Context, in Input) (Output, error) {
	payload, err := readBounded(in.Body, p.opts.MaxInputBytes)
	if err != nil {
		return Output{}, err
	}
	actualMIME := DetectActualMIME(payload)
	if actualMIME == "" {
		return Output{}, ErrUnsupportedMIME
	}
	if in.ContentType != "" && in.ContentType != actualMIME {
		slog.DebugContext(ctx, "imageprocessor.mime_mismatch",
			"declared", in.ContentType,
			"sniffed", actualMIME,
		)
	}

	outMIME := targetMIME(actualMIME, p.opts)

	if p.opts.PreserveSmallImages && canShortCircuit(payload, actualMIME, outMIME, p.opts) {
		cfg, cfgErr := decodeConfig(payload, actualMIME)
		if cfgErr != nil {
			return Output{}, cfgErr
		}
		return Output{
			Body:        bytes.NewReader(payload),
			ContentType: actualMIME,
			SizeBytes:   int64(len(payload)),
			Width:       cfg.Width,
			Height:      cfg.Height,
		}, nil
	}

	img, err := decode(payload, actualMIME)
	if err != nil {
		return Output{}, err
	}
	resized := resize(img, p.opts.MaxLongSidePx)
	encoded, err := encode(resized, outMIME, p.opts)
	if err != nil {
		return Output{}, err
	}
	b := resized.Bounds()
	return Output{
		Body:        bytes.NewReader(encoded),
		ContentType: outMIME,
		SizeBytes:   int64(len(encoded)),
		Width:       b.Dx(),
		Height:      b.Dy(),
	}, nil
}

// targetMIME decides what MIME the encoder should emit for a given input.
// WebP is the only format that ever switches (to JPEG) because we cannot
// re-encode WebP with stdlib + golang.org/x/image.
func targetMIME(inputMIME string, opts Options) string {
	if inputMIME == MimeWebP && opts.RecompressWebPToJPEG {
		return MimeJPEG
	}
	return inputMIME
}

// canShortCircuit reports whether an input is already compact enough that
// recompressing would only add CPU work and quality loss. We require:
//
//   - Input MIME equals output MIME (no transcode required).
//   - Payload size below the configured small-image threshold.
//   - Header-only dimension probe shows both sides ≤ MaxLongSidePx.
func canShortCircuit(payload []byte, inMIME, outMIME string, opts Options) bool {
	if inMIME != outMIME {
		return false
	}
	if int64(len(payload)) > opts.smallImageCap() {
		return false
	}
	if opts.MaxLongSidePx <= 0 {
		return true
	}
	cfg, err := decodeConfig(payload, inMIME)
	if err != nil {
		return false
	}
	return cfg.Width <= opts.MaxLongSidePx && cfg.Height <= opts.MaxLongSidePx
}

// Compile-time assertion that StandardProcessor implements Processor.
var _ Processor = (*StandardProcessor)(nil)
