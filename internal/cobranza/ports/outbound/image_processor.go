//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package outbound

import "github.com/abdimuy/msp-api/internal/platform/imageprocessor"

// ImageProcessor is the cobranza module's view of the platform image
// processor. It is a type alias so the platform package owns the canonical
// shape while consumers depend on a module-local port — keeping the
// hexagonal boundary intact without forcing the app layer to import the
// platform package directly.
//
// For PDF uploads the app layer bypasses the processor entirely (PDF is not
// raster) and stores the blob as-is.
type ImageProcessor = imageprocessor.Processor

// ImageProcessorInput is the request value object passed to
// [ImageProcessor.Process].
type ImageProcessorInput = imageprocessor.Input

// ImageProcessorOutput is the value object returned by
// [ImageProcessor.Process].
type ImageProcessorOutput = imageprocessor.Output
