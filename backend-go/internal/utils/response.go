package utils

import (
	"log"
	"net/http"

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
	setErrorDetail(c, statusCode, message, err, &resp)
	c.JSON(statusCode, resp)
}

// ErrorData sends an error response with structured data payload (useful for 409 duplicate, etc.)
func ErrorData(c *gin.Context, statusCode int, message string, data interface{}, err error) {
	resp := Response{
		Success: false,
		Message: message,
		Data:    data,
	}
	setErrorDetail(c, statusCode, message, err, &resp)
	c.JSON(statusCode, resp)
}

func setErrorDetail(c *gin.Context, statusCode int, message string, err error, resp *Response) {
	if err == nil || resp == nil {
		return
	}
	if statusCode < http.StatusInternalServerError {
		resp.Error = err.Error()
		return
	}

	method := ""
	path := ""
	requestID := ""
	if c != nil && c.Request != nil {
		method = c.Request.Method
		path = c.Request.URL.Path
		requestID = c.GetHeader("X-Request-ID")
	}
	log.Printf("[APIError] status=%d method=%q path=%q request_id=%q message=%q err=%v", statusCode, method, path, requestID, message, err)
}

// SuccessData sends a success response with only data
func SuccessData(c *gin.Context, data interface{}) {
	c.JSON(200, Response{
		Success: true,
		Data:    data,
	})
}
