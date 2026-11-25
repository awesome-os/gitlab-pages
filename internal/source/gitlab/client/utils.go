package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"gitlab.com/gitlab-org/labkit/correlation"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// Reference for below limits: https://www.ietf.org/rfc/rfc1035.txt
const FQDNMaxLength = 255
const DNSLabelMaxLength = 63

var (
	ErrHostnameEmpty   = fmt.Errorf("hostname cannot be empty")
	ErrHostnameToLong  = fmt.Errorf("hostname is too long")
	ErrHostnameInvalid = fmt.Errorf("hostname is invalid")

	regexHostName   = regexp.MustCompile(`^[a-zA-Z0-9._\:\[\]-]+$`)
	protocolDivider = "://"
)

// ValidateHostName Validates the hostname to ensure it is a valid and well-formed URL
func ValidateHostName(hostname string) error {
	// Handle potential protocols in the hostname for backwards compatibility.
	if strings.Contains(hostname, protocolDivider) {
		logging.Logger.Info("hostname has protocol divider")

		if parts := strings.Split(hostname, protocolDivider); len(parts) > 1 {
			hostname = parts[1]
		}
	}

	if hostname == "" {
		return ErrHostnameEmpty
	}

	// Check if the hostname is too long
	if len(hostname) > FQDNMaxLength {
		return ErrHostnameToLong
	}

	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if len(label) > DNSLabelMaxLength {
			return ErrHostnameToLong
		}
	}

	// Check if the hostname contains invalid characters
	if !isValidHostname(hostname) {
		return ErrHostnameInvalid
	}

	return nil
}

// LogInvalidHostname logs a ValidateHostName failure. An empty hostname is
// expected protocol behaviour, since a TLS ClientHello may omit SNI and an
// HTTP/1.0 request may omit Host, so it logs at debug level. Other failures
// mean a malformed hostname and stay at error level.
func LogInvalidHostname(ctx context.Context, err error, attrs ...slog.Attr) {
	all := append([]slog.Attr{
		logv2.Error(err),
		logv2.CorrelationID(correlation.ExtractFromContext(ctx)),
	}, attrs...)

	level := slog.LevelError
	if errors.Is(err, ErrHostnameEmpty) {
		level = slog.LevelDebug
	}

	logv2.FromContext(ctx).LogAttrs(ctx, level, "invalid hostname", all...)
}

// isValidHostname checks if the hostname contains only valid characters (a-z, A-Z, 0-9, ., -, _)
func isValidHostname(hostname string) bool {
	return regexHostName.MatchString(hostname)
}
