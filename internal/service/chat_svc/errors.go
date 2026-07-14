package chat_svc

import (
	"context"
	"net/http"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/utils/httputils"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

type localizedCauseError struct {
	httpErr *httputils.Error
	cause   error
}

func (e *localizedCauseError) Error() string {
	return e.httpErr.Msg
}

func (e *localizedCauseError) Unwrap() error {
	return e.cause
}

func (e *localizedCauseError) As(target any) bool {
	if p, ok := target.(**httputils.Error); ok {
		*p = e.httpErr
		return true
	}
	return false
}

func operationFailedWithCause(ctx context.Context, cause error) error {
	if cause == nil {
		return i18n.NewError(ctx, code.OperationFailed)
	}
	return &localizedCauseError{
		httpErr: &httputils.Error{
			Status: http.StatusBadRequest,
			Code:   code.OperationFailed,
			Msg:    i18n.T(ctx, code.OperationFailed),
		},
		cause: cause,
	}
}
