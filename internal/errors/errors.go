package errors

import (
	stderrors "errors"
	"fmt"
)

const (
	CodeSuccess             int32 = 0
	CodeUnauthorized        int32 = 1001
	CodeTokenExpired        int32 = 1002
	CodeForbidden           int32 = 1003
	CodeInvalidArgument     int32 = 1101
	CodeNotFound            int32 = 1102
	CodeRateLimited         int32 = 1103
	CodeInsufficientCredit  int32 = 2001
	CodeGenerationExpired   int32 = 2002
	CodeGenerationCompleted int32 = 2003
	CodeDuplicateRequest    int32 = 2004
	CodeUploadTokenFailed   int32 = 3001
	CodeInvalidFileType     int32 = 3002
	CodeFileTooLarge        int32 = 3003
	CodeInternal            int32 = 5000
)

type AppError struct {
	Code    int32
	Message string
	Cause   error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

func New(code int32, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func Wrap(code int32, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Cause: cause}
}

func InvalidArgument(msg string) *AppError {
	return &AppError{Code: CodeInvalidArgument, Message: msg}
}

func NotFound(msg string) *AppError {
	return &AppError{Code: CodeNotFound, Message: msg}
}

func Unauthorized(msg string) *AppError {
	return &AppError{Code: CodeUnauthorized, Message: msg}
}

func Forbidden(msg string) *AppError {
	return &AppError{Code: CodeForbidden, Message: msg}
}

func InsufficientCredits(balance, need int) *AppError {
	return &AppError{
		Code:    CodeInsufficientCredit,
		Message: fmt.Sprintf("insufficient credits: have %d, need %d", balance, need),
	}
}

func GenerationExpired() *AppError {
	return &AppError{Code: CodeGenerationExpired, Message: "generation expired"}
}

func Internal(msg string, cause error) *AppError {
	return &AppError{Code: CodeInternal, Message: msg, Cause: cause}
}

func InvalidFileType(msg string) *AppError {
	return &AppError{Code: CodeInvalidFileType, Message: msg}
}

func FileTooLarge(maxSize int64) *AppError {
	return &AppError{Code: CodeFileTooLarge, Message: fmt.Sprintf("file too large, max %d bytes", maxSize)}
}

func IsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	var appErr *AppError
	if !stderrors.As(err, &appErr) {
		return nil, false
	}
	return appErr, true
}
