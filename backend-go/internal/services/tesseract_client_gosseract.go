//go:build cgo

package services

import "github.com/otiai10/gosseract/v2"

func newTesseractClient() (tesseractClient, error) {
	return gosseract.NewClient(), nil
}

