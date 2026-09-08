package localstorage

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

func noisyImage(width, height int) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	source := rand.New(rand.NewSource(1))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			result.SetNRGBA(x, y, color.NRGBA{
				R: uint8(source.Intn(256)),
				G: uint8(source.Intn(256)),
				B: uint8(source.Intn(256)),
				A: 255,
			})
		}
	}
	return result
}

// 编码后超出预算的页面必须逐级降采样，否则 Provider 会直接拒收整份文档且不重试。
func TestNormalizedPageIsDownscaledUntilItFitsTheByteBudget(t *testing.T) {
	t.Parallel()
	image, encoded, err := encodeNormalizedPage(noisyImage(2400, 1800))
	if err != nil {
		t.Fatal(err)
	}
	bounds := image.Bounds()
	if len(encoded) > normalizedPageByteBudget && bounds.Dx() > normalizedFloorEdge {
		t.Fatalf("超出预算却没有继续降采样: %dx%d %d 字节", bounds.Dx(), bounds.Dy(), len(encoded))
	}
	if bounds.Dx() >= 2400 {
		t.Fatalf("高熵页面未被降采样: %dx%d", bounds.Dx(), bounds.Dy())
	}
	if bounds.Dx() < normalizedFloorEdge {
		t.Fatalf("降采样越过了下限: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

// 常见截图本来就在预算内，必须原样通过，不能被无谓地重采样。
func TestNormalizedPageKeepsSmallPagesUntouched(t *testing.T) {
	t.Parallel()
	source := patternedFingerprintImage(true)
	result, encoded, err := encodeNormalizedPage(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds().Dx() != source.Bounds().Dx() || result.Bounds().Dy() != source.Bounds().Dy() {
		t.Fatalf("小页面被改变了尺寸: %v -> %v", source.Bounds(), result.Bounds())
	}
	if len(encoded) == 0 {
		t.Fatal("没有产出编码结果")
	}
}
