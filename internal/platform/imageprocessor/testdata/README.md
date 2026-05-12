# imageprocessor test fixtures

This directory intentionally holds no binary fixtures.

All test images are generated programmatically inside `*_test.go` using:

- `image.NewRGBA` to create a canvas
- `image/jpeg.Encode`, `image/png.Encode`, `image/gif.Encode` to serialize

Why no checked-in blobs:

1. Keeps the repo small.
2. Eliminates "where did this PNG come from" debates.
3. Makes the dimensions and content choices visible and reviewable in code.

If a future test needs a WebP fixture, add a small helper in the test file
that uses the official Google libwebp sample bytes (a 1×1 transparent WebP
is ~30 bytes and can be hex-literal-encoded in the test source).
