package response

import (
	"net/http"

	"PingMe/internal/errorcode"
)

// Response represents a standard API response
type Response struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// Success returns a success response
func Success(data map[string]interface{}) Response {
	return Response{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

// SuccessWithMessage returns a success response with a custom message
func SuccessWithMessage(msg string, data map[string]interface{}) Response {
	return Response{
		Code:    0,
		Message: msg,
		Data:    data,
	}
}

// Fail returns a failure response with error code
func Fail(err *errorcode.Code) Response {
	return Response{
		Code:    http.StatusOK, // Use 200 for API compatibility, real status in code
		Message: err.Message,
		Data:    err.ToMap(),
	}
}

// FailWithMessage returns a failure response with custom message
func FailWithMessage(msg string, code int) Response {
	return Response{
		Code:    code,
		Message: msg,
	}
}
