package errortracking

import (
	"net/http"

	"gitlab.com/gitlab-org/labkit/errortracking"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// CaptureOption alias to avoid importing labkit/errortracking in internal packages
type CaptureOption = errortracking.CaptureOption

// WithField alias to avoid importing labkit/errortracking in internal packages
func WithField(key, value string) CaptureOption {
	return errortracking.WithField(key, value)
}

// CaptureErrWithReqAndStackTrace calls labkit's errortracking function and attaches the request, stack trace and any additional fields
func CaptureErrWithReqAndStackTrace(err error, r *http.Request, fields ...errortracking.CaptureOption) {
	opts := append(
		fields,
		errortracking.WithContext(r.Context()),
		errortracking.WithRequest(requestForErrorTracking(r)),
		errortracking.WithStackTrace(),
	)

	errortracking.Capture(logging.CleanURLError(err), opts...)
}

func requestForErrorTracking(r *http.Request) *http.Request {
	return &http.Request{
		Method: r.Method,
		URL:    logging.CleanURLValue(r.URL),
		Host:   r.Host,
	}
}

// CaptureErrWithStackTrace calls labkit's errortracking function and attaches the stack trace and any additional fields
func CaptureErrWithStackTrace(err error, fields ...errortracking.CaptureOption) {
	opts := append(
		fields,
		errortracking.WithStackTrace(),
	)

	errortracking.Capture(logging.CleanURLError(err), opts...)
}
