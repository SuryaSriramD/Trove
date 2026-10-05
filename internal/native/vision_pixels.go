package native

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"io"
)

// measureVisionSource abstains only for exact equality at decoded 16-bit RGBA
// precision, or zero alpha at every source pixel. There is no tolerance, sample,
// percentile or downscaled blankness heuristic. A single faint/low-alpha pixel
// prevents an opaque uniform classification, even when normalization loses it.
func measureVisionSource(ctx context.Context, source image.Image) (string, error) {
	bounds := source.Bounds()
	first := color.NRGBA64Model.Convert(source.At(bounds.Min.X, bounds.Min.Y)).(color.NRGBA64)
	uniform, transparent := true, true
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if (x-bounds.Min.X)%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return "", err
				}
			}
			pixel := color.NRGBA64Model.Convert(source.At(x, y)).(color.NRGBA64)
			uniform = uniform && pixel == first
			transparent = transparent && pixel.A == 0
			// Either condition can only become false. Continuing has no bearing on the
			// decision once both fail; normalization still decodes the source as before.
			if !uniform && !transparent {
				return "", nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("Source pixel measurement (%d × %d decoded pixels, before resizing and alpha composition; encoded orientation): ", bounds.Dx(), bounds.Dy())
	if transparent {
		return prefix + "every source pixel is fully transparent. Hidden RGB values are not visible content. No visible objects or text can be identified. No model interpretation or OCR was attempted.", nil
	}
	if first == (color.NRGBA64{0xffff, 0xffff, 0xffff, 0xffff}) {
		return prefix + "the image is exactly uniform white (blank at decoded-pixel level). No objects or text can be identified from spatial pixel variation. No model interpretation or OCR was attempted.", nil
	}
	return prefix + fmt.Sprintf("every source pixel has the same 16-bit RGB value #%04x%04x%04x and alpha %d/65535. This uniform color has no spatial pixel variation to identify objects or text. No model interpretation or OCR was attempted.", first.R, first.G, first.B, first.A), nil
}

type visionContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r visionContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type visionContextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w visionContextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}
