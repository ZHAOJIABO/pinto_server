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
	CodeTaskQuotaExceeded   int32 = 2005
	CodeWorkUnderReview     int32 = 2006
	CodeBlindBoxQuotaUsedUp int32 = 2007
	CodeUploadTokenFailed   int32 = 3001
	CodeInvalidFileType     int32 = 3002
	CodeFileTooLarge        int32 = 3003
	CodeInternal            int32 = 5000
)

// 4xxx 段专属于管理后台草稿流程。Web 端按码值分支（admin-template-draft-api.md
// 第 8 节明确要求不靠 message 文案匹配），所以这些数值本身就是契约，不得调整或复用。
const (
	CodeDraftConflict       int32 = 4001
	CodeDraftNotFound       int32 = 4002
	CodeDraftLimitExceeded  int32 = 4003
	CodeDraftNotPublishable int32 = 4004
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

func TaskQuotaExceeded(current, limit int) *AppError {
	return &AppError{
		Code:    CodeTaskQuotaExceeded,
		Message: fmt.Sprintf("task quota exceeded: %d running or queued, limit %d", current, limit),
	}
}

// BlindBoxQuotaUsedUp 报告当日盲盒次数已用尽。刻意不复用 CodeRateLimited：那个码表示
// "请求太频繁，稍后重试"，而这里要到次日零点才恢复，客户端要弹的文案和重试策略都不同。
func BlindBoxQuotaUsedUp(limit int) *AppError {
	return &AppError{
		Code:    CodeBlindBoxQuotaUsedUp,
		Message: fmt.Sprintf("daily blind box quota used up: limit %d", limit),
	}
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

// DraftConflict 报告草稿的乐观锁失配。message 里带上抢先写入的管理员，仅用于日志；
// 前端要展示的 updatedByActor 由它收到 4001 后重新拉详情取得。
func DraftConflict(actor string) *AppError {
	if actor == "" {
		return &AppError{Code: CodeDraftConflict, Message: "draft was modified by another administrator"}
	}
	return &AppError{Code: CodeDraftConflict, Message: fmt.Sprintf("draft was modified by %s", actor)}
}

func DraftNotFound() *AppError {
	return &AppError{Code: CodeDraftNotFound, Message: "draft not found"}
}

func DraftLimitExceeded(limit int) *AppError {
	return &AppError{
		Code:    CodeDraftLimitExceeded,
		Message: fmt.Sprintf("draft limit reached (%d); discard or publish an existing draft first", limit),
	}
}

// DraftNotPublishable 覆盖两类「草稿存在但现在发不出去」：必填项没补全，以及
// 关联的已发布模板在草稿存活期间被下架。都不能报 4002，否则管理员会看到
// 「草稿不存在」而草稿明明就在箱里。
func DraftNotPublishable(msg string) *AppError {
	return &AppError{Code: CodeDraftNotPublishable, Message: msg}
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
