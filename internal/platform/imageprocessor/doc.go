// Package imageprocessor provides a reusable, pure-Go image-processing
// pipeline (decode → resize → re-encode) that domain modules consume via
// an outbound port.
//
// # Role in the architecture
//
// Modules that accept image uploads (ventas evidence, cobranza receipts,
// INE photos) MUST pass every upload through a [Processor] before handing
// the bytes to a storage provider. The package guarantees:
//
//   - Bounded memory: inputs above [Options.MaxInputBytes] are rejected
//     before any decode happens.
//   - MIME safety: the MIME is detected from content (first 512 bytes via
//     [net/http.DetectContentType]) and validated against an allow-list,
//     so a buggy client cannot lie about the content type.
//   - Quality knobs: long-side cap, JPEG quality, PNG compression, and an
//     opt-out for already-compact inputs.
//   - Deterministic outputs: callers always receive a [bytes.Reader] over
//     a fully buffered payload — no half-streamed bodies, no leaked file
//     handles.
//
// # Public API
//
//   - [Processor] — the port that consumers depend on.
//   - [Input] / [Output] — request and response value objects.
//   - [Options] — configuration for [StandardProcessor].
//   - [StandardProcessor] — production pipeline.
//   - [NoOpProcessor] — passthrough; used in tests and via the
//     IMAGEPROCESSOR_ENABLED=false runtime opt-out.
//   - [New] — factory that selects an implementation from
//     [github.com/abdimuy/msp-api/internal/platform/config.ImageProcessor].
//
// # Supported formats
//
// Decode: JPEG, PNG, GIF, WebP (read-only via golang.org/x/image/webp).
// Encode: JPEG, PNG, GIF (single frame). WebP inputs are re-encoded as
// JPEG when [Options.RecompressWebPToJPEG] is true.
//
// HEIC/HEIF inputs are intentionally not supported: they require CGO
// libraries which the Windows Server 2016 cross-compile target rules out.
// Mobile clients must transcode to JPEG before upload.
//
// # Constraints
//
// Pure Go. The only non-stdlib dependency is golang.org/x/image. Every
// allocation is bounded by [Options.MaxInputBytes] (source bytes) plus
// the working RGBA buffer dictated by the decoded image's dimensions —
// for a 4032x3024 phone photo that is ~50 MB, acceptable for the
// single-server on-prem deploy.
package imageprocessor
