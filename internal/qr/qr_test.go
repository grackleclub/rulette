package qr

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncode(t *testing.T) {
	text := "https://rulette.grackle.club/abcdef"

	img, err := Encode(text, 0)
	require.NoError(t, err)
	require.NotNil(t, img)

	b := img.Bounds()
	require.Greater(t, b.Dx(), 0)
	require.Greater(t, b.Dy(), 0)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.Greater(t, buf.Len(), 0)

	if dir := os.Getenv("QR_OUT_DIR"); dir != "" {
		path := filepath.Join(dir, "qr_test.png")
		require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
		t.Logf("wrote %s", path)
	}
}

func TestEncode_DefaultScale(t *testing.T) {
	img, err := Encode("hi", 0)
	require.NoError(t, err)
	img2, err := Encode("hi", defaultScale)
	require.NoError(t, err)
	require.Equal(t, img.Bounds(), img2.Bounds())
}
