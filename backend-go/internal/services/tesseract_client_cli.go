//go:build !cgo

package services

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/otiai10/gosseract/v2"
)

type tesseractCLIClient struct {
	imagePath string
	langs     []string
	psm       gosseract.PageSegMode
	whitelist string
}

func newTesseractClient() (tesseractClient, error) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return nil, fmt.Errorf("tesseract not found in PATH: %w", err)
	}
	return &tesseractCLIClient{
		langs: []string{"eng"},
		psm:   gosseract.PSM_AUTO,
	}, nil
}

func (c *tesseractCLIClient) Close() error { return nil }

func (c *tesseractCLIClient) SetLanguage(langs ...string) error {
	if len(langs) == 0 {
		return fmt.Errorf("languages cannot be empty")
	}
	c.langs = langs
	return nil
}

func (c *tesseractCLIClient) SetPageSegMode(mode gosseract.PageSegMode) error {
	c.psm = mode
	return nil
}

func (c *tesseractCLIClient) SetWhitelist(whitelist string) error {
	c.whitelist = whitelist
	return nil
}

func (c *tesseractCLIClient) SetImage(imagePath string) error {
	c.imagePath = imagePath
	return nil
}

func (c *tesseractCLIClient) Text() (string, error) {
	if strings.TrimSpace(c.imagePath) == "" {
		return "", fmt.Errorf("image path is empty")
	}

	args := []string{c.imagePath, "stdout"}
	if len(c.langs) > 0 {
		args = append(args, "-l", strings.Join(c.langs, "+"))
	}
	args = append(args, "--psm", strconv.Itoa(int(c.psm)))

	if strings.TrimSpace(c.whitelist) != "" {
		args = append(args, "-c", "tessedit_char_whitelist="+c.whitelist)
	}

	out, err := exec.Command("tesseract", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tesseract failed: %w (output: %s)", err, string(out))
	}

	return string(out), nil
}

