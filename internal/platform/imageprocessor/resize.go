package imageprocessor

import (
	"image"

	"golang.org/x/image/draw"
)

// resize scales src so its longer side equals maxLongSide while preserving
// aspect ratio. If the source is already within the cap (or maxLongSide is
// zero / negative), src is returned unchanged. Uses CatmullRom for best
// quality among the stdlib-friendly kernels exposed by golang.org/x/image.
func resize(src image.Image, maxLongSide int) image.Image {
	if maxLongSide <= 0 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxLongSide && h <= maxLongSide {
		return src
	}
	var newW, newH int
	if w >= h {
		newW = maxLongSide
		newH = (h * maxLongSide) / w
		if newH < 1 {
			newH = 1
		}
	} else {
		newH = maxLongSide
		newW = (w * maxLongSide) / h
		if newW < 1 {
			newW = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}
