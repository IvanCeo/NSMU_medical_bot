package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func expectedError(err error) bool {
	for _, kind := range []error{domain.ErrStorage, domain.ErrTelegramUnavailable, domain.ErrTelegramUnauthorized, domain.ErrTelegramConflict} {
		if errors.Is(err, kind) {
			return false
		}
	}
	for _, kind := range []error{domain.ErrForbidden, domain.ErrNotInGroup, domain.ErrNotFound, domain.ErrConflict, domain.ErrInvalidInput, domain.ErrExpiredDialog, domain.ErrInvalidUser} {
		if errors.Is(err, kind) {
			return true
		}
	}
	return false
}

func (r *Router) logFailure(ctx context.Context, event, action string, actor int64, err error) {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return
	}
	kind := "unexpected"
	switch {
	case errors.Is(err, domain.ErrStorage):
		kind = "storage"
	case errors.Is(err, domain.ErrTelegramUnavailable):
		kind = "telegram_unavailable"
	case errors.Is(err, domain.ErrTelegramUnauthorized):
		kind = "telegram_unauthorized"
	case errors.Is(err, domain.ErrTelegramConflict):
		kind = "telegram_conflict"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "deadline"
	}
	attrs := []any{"event", event, "action", action, "telegram_id", actor, "error_kind", kind, "error_type", fmt.Sprintf("%T", err)}
	var request *requestError
	if errors.As(err, &request) {
		attrs = append(attrs, "telegram_code", request.code, "telegram_reason", request.reason)
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		attrs = append(attrs, "storage_code", coded.Code())
	}
	if expectedError(err) {
		r.logger.DebugContext(ctx, "Operation rejected", attrs...)
		return
	}
	r.logger.ErrorContext(ctx, "Operation failed", attrs...)
}

func callbackAction(data string) string {
	for _, action := range []string{"menu", "tasks", "subject", "view", "done", "undo", "snooze", "dlg"} {
		if data == action || strings.HasPrefix(data, action+":") {
			return action
		}
	}
	return "invalid"
}
