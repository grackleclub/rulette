// Package qr renders QR codes with a human-readable caption below.
package qr

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"rsc.io/qr"
)

const (
	defaultScale = 16
	padding      = 24
	captionGap   = 16
	fontSize     = 36
	maxWidth     = 800
	maxHeight    = 850
)

var captionFont = func() font.Face {
	tt, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(fmt.Errorf("parse font: %w", err))
	}
	face, err := opentype.NewFace(tt, &opentype.FaceOptions{
		Size: fontSize,
		DPI:  72,
	})
	if err != nil {
		panic(fmt.Errorf("new face: %w", err))
	}
	return face
}()

// Encode returns an image of the QR code for text with text rendered as a
// caption beneath the code. scale controls pixels-per-QR-module; if scale
// is <= 0, a default is used.
func Encode(text string, scale int) (*image.RGBA, error) {
	if scale <= 0 {
		scale = defaultScale
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("encode qr: %w", err)
	}

	textW := font.MeasureString(captionFont, text).Ceil()
	textH := captionFont.Metrics().Height.Ceil()

	if minScale := (textW + code.Size - 1) / code.Size; minScale > scale {
		scale = minScale
	}
	if maxScaleW := (maxWidth - 2*padding) / code.Size; scale > maxScaleW {
		scale = maxScaleW
	}
	if maxScaleH := (maxHeight - 2*padding - captionGap - textH) / code.Size; scale > maxScaleH {
		scale = maxScaleH
	}
	qrSide := code.Size * scale

	canvasW := qrSide + 2*padding
	if w := textW + 2*padding; w > canvasW {
		canvasW = w
	}
	if canvasW > maxWidth {
		canvasW = maxWidth
	}
	canvasH := qrSide + 2*padding + captionGap + textH
	if canvasH > maxHeight {
		canvasH = maxHeight
	}

	img := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	qrX := (canvasW - qrSide) / 2
	black := &image.Uniform{C: color.Black}
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if !code.Black(x, y) {
				continue
			}
			r := image.Rect(
				qrX+x*scale, padding+y*scale,
				qrX+(x+1)*scale, padding+(y+1)*scale,
			)
			draw.Draw(img, r, black, image.Point{}, draw.Src)
		}
	}

	textX := (canvasW - textW) / 2
	textY := padding + qrSide + captionGap + captionFont.Metrics().Ascent.Ceil()
	(&font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: color.Black},
		Face: captionFont,
		Dot:  fixed.P(textX, textY),
	}).DrawString(text)

	return img, nil
}
