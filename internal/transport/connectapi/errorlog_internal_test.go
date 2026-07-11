package connectapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

func runWithErrorLog(t *testing.T, retErr error) string {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	wrapped := NewErrorLogInterceptor(logger)(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, retErr
	})
	_, _ = wrapped(context.Background(), connect.NewRequest(&portcullisv1.AuditListRequest{}))
	return buf.String()
}

func TestErrorLogRecordsServerFault(t *testing.T) {
	out := runWithErrorLog(t, connect.NewError(connect.CodeInternal, errors.New("boom")))
	if !strings.Contains(out, "rpc failed") || !strings.Contains(out, "internal") {
		t.Errorf("server-fault error not logged: %q", out)
	}
	// The generic-but-possibly-sensitive message body must not be logged.
	if strings.Contains(out, "boom") {
		t.Errorf("error message body leaked into the log: %q", out)
	}
}

func TestErrorLogIgnoresClientFault(t *testing.T) {
	for _, code := range []connect.Code{connect.CodePermissionDenied, connect.CodeUnauthenticated, connect.CodeInvalidArgument} {
		if out := runWithErrorLog(t, connect.NewError(code, errors.New("nope"))); out != "" {
			t.Errorf("client-fault %v was logged: %q", code, out)
		}
	}
}

func TestErrorLogIgnoresSuccess(t *testing.T) {
	if out := runWithErrorLog(t, nil); out != "" {
		t.Errorf("successful RPC produced a log line: %q", out)
	}
}
