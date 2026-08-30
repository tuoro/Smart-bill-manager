package httpapi

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

type spaHandler struct {
	files      fs.FS
	fileServer http.Handler
}

func newSPAHandler(root string) (http.Handler, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("web distribution path is required")
	}
	directory := os.DirFS(root)
	info, err := fs.Stat(directory, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("index.html must be a regular file")
	}
	return spaHandler{files: directory, fileServer: http.FileServerFS(directory)}, nil
}

func (s spaHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	cleanPath := path.Clean("/" + request.URL.Path)
	relativePath := strings.TrimPrefix(cleanPath, "/")
	if relativePath != "" {
		info, err := fs.Stat(s.files, relativePath)
		if err == nil && info.Mode().IsRegular() {
			if strings.HasPrefix(relativePath, "assets/") {
				response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				response.Header().Set("Cache-Control", "no-store")
			}
			s.fileServer.ServeHTTP(response, request)
			return
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			http.Error(response, "web asset unavailable", http.StatusInternalServerError)
			return
		}
		if relativePath == "assets" || strings.HasPrefix(relativePath, "assets/") || path.Ext(relativePath) != "" {
			http.NotFound(response, request)
			return
		}
	}
	response.Header().Set("Cache-Control", "no-store")
	http.ServeFileFS(response, request, s.files, "index.html")
}
