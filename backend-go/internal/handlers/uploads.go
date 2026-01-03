package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type UploadsHandler struct {
	uploadsDir string
}

func NewUploadsHandler(uploadsDir string) *UploadsHandler {
	return &UploadsHandler{uploadsDir: uploadsDir}
}

func (h *UploadsHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/*filepath", h.GetFile)
}

func (h *UploadsHandler) GetFile(c *gin.Context) {
	p := strings.TrimSpace(c.Param("filepath"))
	if p == "" || p == "/" {
		c.Status(http.StatusNotFound)
		return
	}
	// Stored paths are typically "uploads/<file>", so normalize.
	stored := strings.TrimPrefix(p, "/")
	abs, err := resolveUploadsFilePath(h.uploadsDir, "uploads/"+stored)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if _, err := os.Stat(abs); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	// Uploaded files may contain sensitive information; prevent shared caching.
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(abs)
}
