package localstorage

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/draw"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestPageVisualFingerprintIsDeterministicAndSensitiveToLayout(t *testing.T) {
	leftDark := patternedFingerprintImage(true)
	first := pageVisualFingerprint(leftDark)
	second := pageVisualFingerprint(leftDark)
	if first != second {
		t.Fatalf("fingerprint changed for the same pixels: %#v != %#v", first, second)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}

	different := pageVisualFingerprint(patternedFingerprintImage(false))
	if first.DHash64 == different.DHash64 && first.AHash64 == different.AHash64 {
		t.Fatalf("mirrored layouts share both visual hashes: %#v", first)
	}
}

func TestPageVisualFingerprintCompositesTransparencyOnWhite(t *testing.T) {
	transparent := image.NewNRGBA(image.Rect(0, 0, 18, 16))
	opaqueWhite := image.NewNRGBA(image.Rect(0, 0, 18, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 18; x++ {
			opaqueWhite.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	if got, want := pageVisualFingerprint(transparent), pageVisualFingerprint(opaqueWhite); got != want {
		t.Fatalf("transparent white composite = %#v, want %#v", got, want)
	}
}

func TestPageVisualFingerprintMatchesApprovedReencodingAndEqualScaling(t *testing.T) {
	source := patternedFingerprintImage(true)
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, source); err != nil {
		t.Fatal(err)
	}
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	pngImage, _, err := image.Decode(bytes.NewReader(pngBytes.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	jpegImage, _, err := image.Decode(bytes.NewReader(jpegBytes.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	assertFingerprintImagesNear(t, pngImage, jpegImage)

	scaled := image.NewNRGBA(image.Rect(0, 0, source.Bounds().Dx()*10, source.Bounds().Dy()*10))
	draw.NearestNeighbor.Scale(scaled, scaled.Bounds(), source, source.Bounds(), draw.Src, nil)
	assertFingerprintImagesNear(t, source, scaled)
}

func patternedFingerprintImage(leftDark bool) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, 18, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 18; x++ {
			dark := x < 9
			if !leftDark {
				dark = !dark
			}
			shade := uint8(240)
			if dark {
				shade = uint8(15 + y)
			}
			result.SetNRGBA(x, y, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	return result
}

func assertFingerprintImagesNear(t *testing.T, left, right image.Image) {
	t.Helper()
	leftBounds := left.Bounds()
	rightBounds := right.Bounds()
	near, _, _, err := domain.VisualPagesNear(
		domain.VisualPage{
			ID: "left", DocumentID: "left-document", PageNumber: 1,
			Width: leftBounds.Dx(), Height: leftBounds.Dy(), Fingerprint: pageVisualFingerprint(left),
		},
		domain.VisualPage{
			ID: "right", DocumentID: "right-document", PageNumber: 1,
			Width: rightBounds.Dx(), Height: rightBounds.Dy(), Fingerprint: pageVisualFingerprint(right),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !near {
		t.Fatal("approved reencoding or equal scaling exceeded the frozen visual thresholds")
	}
}
