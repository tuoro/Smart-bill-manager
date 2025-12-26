package services

import "github.com/otiai10/gosseract/v2"

type tesseractClient interface {
	Close() error
	SetLanguage(langs ...string) error
	SetPageSegMode(mode gosseract.PageSegMode) error
	SetImage(imagePath string) error
	Text() (string, error)
	SetWhitelist(whitelist string) error
}
