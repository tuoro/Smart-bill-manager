package localstorage

import (
	"image"
	"image/color"
	imagedraw "image/draw"

	"golang.org/x/image/draw"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func pageVisualFingerprint(source image.Image) domain.PageVisualFingerprint {
	opaque := compositeOnWhite(source)
	differenceSample := resizeFingerprintSample(opaque, 9, 8)
	var dhash uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			dhash <<= 1
			if visualLuma(differenceSample.At(x, y)) > visualLuma(differenceSample.At(x+1, y)) {
				dhash |= 1
			}
		}
	}
	averageSample := resizeFingerprintSample(opaque, 8, 8)
	lumas := [64]int{}
	sum := 0
	for index := range lumas {
		x, y := index%8, index/8
		lumas[index] = visualLuma(averageSample.At(x, y))
		sum += lumas[index]
	}
	average := sum / len(lumas)
	var ahash uint64
	for _, luma := range lumas {
		ahash <<= 1
		if luma >= average {
			ahash |= 1
		}
	}
	return domain.NewPageVisualFingerprint(dhash, ahash)
}

func compositeOnWhite(source image.Image) *image.RGBA {
	bounds := source.Bounds()
	result := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	imagedraw.Draw(result, result.Bounds(), image.NewUniform(color.White), image.Point{}, imagedraw.Src)
	imagedraw.Draw(result, result.Bounds(), source, bounds.Min, imagedraw.Over)
	return result
}

func resizeFingerprintSample(source image.Image, width, height int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(result, result.Bounds(), source, source.Bounds(), draw.Src, nil)
	return result
}

func visualLuma(value color.Color) int {
	red, green, blue, _ := value.RGBA()
	return (299*int(red>>8) + 587*int(green>>8) + 114*int(blue>>8) + 500) / 1000
}
