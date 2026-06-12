package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"

	"github.com/grackleclub/rulette/internal/qr"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const maxlenDNS = 253

// brandFont is the cursive face used for the "rulette" banner above the QR,
// the same Borel font the page logo uses. parsed once from the embedded FS.
var brandFont = func() *opentype.Font {
	data, err := static.ReadFile("static/fonts/Borel-Regular.ttf")
	if err != nil {
		panic(fmt.Errorf("read brand font: %w", err))
	}
	f, err := opentype.Parse(data)
	if err != nil {
		panic(fmt.Errorf("parse brand font: %w", err))
	}
	return f
}()

// brandBanner renders a wide, dark "rulette" cursive banner the given pixels
// wide, sized so the word spans most of the width. white text on the dark
// brand color, matching the on-page logo.
func brandBanner(width int) (*image.RGBA, error) {
	const word = "rulette"
	// size the word so it spans this fraction of the QR width (full-width look);
	// the band height is then cropped tight to the painted pixels below.
	const targetFrac = 0.72
	const trial = 64.0

	measure, err := opentype.NewFace(brandFont, &opentype.FaceOptions{Size: trial, DPI: 72})
	if err != nil {
		return nil, fmt.Errorf("brand face (trial): %w", err)
	}
	trialW := font.MeasureString(measure, word).Ceil()
	_ = measure.Close()
	if trialW == 0 {
		return nil, fmt.Errorf("brand font measured zero width")
	}

	size := trial * (float64(width) * targetFrac) / float64(trialW)
	face, err := opentype.NewFace(brandFont, &opentype.FaceOptions{Size: size, DPI: 72})
	if err != nil {
		return nil, fmt.Errorf("brand face: %w", err)
	}
	defer face.Close()

	dark := color.RGBA{R: 0x1b, G: 0x11, B: 0x0f, A: 0xff} // --color-text-dark
	m := face.Metrics()
	textW := font.MeasureString(face, word).Ceil()

	// render onto a tall scratch canvas, then crop to the rows actually painted.
	// the font reports descender room that "rulette" never uses, which is what
	// left a gap under the word; trusting the ink itself avoids it.
	scratchH := m.Ascent.Ceil() + m.Descent.Ceil() + int(size)
	scratch := image.NewRGBA(image.Rect(0, 0, width, scratchH))
	draw.Draw(scratch, scratch.Bounds(), &image.Uniform{C: dark}, image.Point{}, draw.Src)
	(&font.Drawer{
		Dst:  scratch,
		Src:  &image.Uniform{C: color.White},
		Face: face,
		Dot:  fixed.P((width-textW)/2, m.Ascent.Ceil()),
	}).DrawString(word)

	top, bottom := -1, -1
	for y := range scratchH {
		for x := range width {
			if r, _, _, _ := scratch.At(x, y).RGBA(); r>>8 > 0x40 { // white-ish ink
				if top == -1 {
					top = y
				}
				bottom = y
				break
			}
		}
	}
	if top == -1 { // nothing painted; fall back to a baseline-height slice
		top, bottom = 0, m.Ascent.Ceil()
	}

	pad := int(float64(width) * 0.02) // slim, equal breathing room top and bottom
	inkH := bottom - top + 1
	img := image.NewRGBA(image.Rect(0, 0, width, inkH+2*pad))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: dark}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, pad, width, pad+inkH), scratch, image.Point{Y: top}, draw.Src)
	return img, nil
}

// qrHandler serves a PNG QR code for the game's join URL, topped with a
// "rulette" banner. Only available while the game is in the invite (pre-game)
// state.
func qrHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("game_id")
	log := log.With("handler", "qrHandler", "game_id", gameID)

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			log.Warn("game not found")
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		log.Error("fetch game state", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// state 0 = created, 1 = inviting; both are pre-game
	if state.Game.StateID != stateCreated && state.Game.StateID != stateInviting {
		log.Warn("qr requested outside invite state",
			"state_id", state.Game.StateID,
		)
		http.Error(w, "game not accepting invites", http.StatusConflict)
		return
	}

	if len(r.Host) > maxlenDNS {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	joinURL := fmt.Sprintf("%s/%s/join", baseURL(r), gameID)

	code, err := qr.Encode(joinURL, 0)
	if err != nil {
		log.Error("encode qr", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// stack the banner on top of the QR code on one canvas
	banner, err := brandBanner(code.Bounds().Dx())
	if err != nil {
		log.Error("render banner", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	bandH := banner.Bounds().Dy()
	img := image.NewRGBA(image.Rect(0, 0, code.Bounds().Dx(), bandH+code.Bounds().Dy()))
	draw.Draw(img, banner.Bounds(), banner, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, bandH, img.Bounds().Dx(), img.Bounds().Dy()),
		code, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Error("encode png", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Error("write response", "error", err)
	}
}
