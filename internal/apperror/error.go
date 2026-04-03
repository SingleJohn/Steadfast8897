package apperror

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

type AppError struct {
	Code    int    `json:"-"`
	Message string `json:"message"`
}

func (e *AppError) Error() string {
	return e.Message
}

func NotFound() *AppError {
	return &AppError{Code: http.StatusNotFound, Message: "Not found"}
}

func Unauthorized() *AppError {
	return &AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"}
}

func Forbidden(msg string) *AppError {
	return &AppError{Code: http.StatusForbidden, Message: msg}
}

func BadRequest(msg string) *AppError {
	return &AppError{Code: http.StatusBadRequest, Message: msg}
}

func Internal(err error) *AppError {
	slog.Error("Internal error", "error", err)
	return &AppError{Code: http.StatusInternalServerError, Message: "Internal error"}
}

func DBError(err error) *AppError {
	slog.Error("Database error", "error", err)
	return &AppError{Code: http.StatusInternalServerError, Message: "Internal error"}
}

func WriteJSON(c *gin.Context, err *AppError) {
	c.JSON(err.Code, gin.H{"message": err.Message})
	c.Abort()
}

func Wrap(err error) *AppError {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*AppError); ok {
		return ae
	}
	return Internal(fmt.Errorf("%w", err))
}
