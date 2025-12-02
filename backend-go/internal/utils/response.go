package utils

import (
	"github.com/gin-gonic/gin"
)

// Response represents a standard API response
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Success sends a success response
func Success(c *gin.Context, statusCode int, message string, data interface{}) {
	c.JSON(statusCode, Response{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Error sends an error response
func Error(c *gin.Context, statusCode int, message string, err error) {
	resp := Response{
		Success: false,
		Message: message,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	c.JSON(statusCode, resp)
}

// SuccessData sends a success response with only data
func SuccessData(c *gin.Context, data interface{}) {
	c.JSON(200, Response{
		Success: true,
		Data:    data,
	})
}
