package client

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
	logtest "gitlab.com/gitlab-org/labkit/v2/testing/logtest"
)

func TestValidateHostName(t *testing.T) {
	tests := []struct {
		name      string
		hostname  string
		wantError error
		isV6Addr  bool
	}{
		{
			name:      "Empty hostname",
			hostname:  "",
			wantError: fmt.Errorf("hostname cannot be empty"),
		},
		{
			name:      "Valid hostname",
			hostname:  "example.com",
			wantError: nil,
		},
		{
			name:      "Valid short IPv6 as host",
			hostname:  "::1",
			wantError: nil,
			isV6Addr:  true,
		},
		{
			name:      "Valid full length IPv6 as host",
			hostname:  "2001:db8:3333:4444:5555:6666:7777:8888",
			wantError: nil,
			isV6Addr:  true,
		},
		{
			name:      "Hostname too long",
			hostname:  strings.Repeat("a", FQDNMaxLength+1) + ".com",
			wantError: ErrHostnameToLong,
		},
		{
			name:      "Label too long",
			hostname:  strings.Repeat("a", DNSLabelMaxLength+1) + ".com",
			wantError: ErrHostnameToLong,
		},
		{
			name:      "Invalid characters in hostname",
			hostname:  "example!.com",
			wantError: ErrHostnameInvalid,
		},
		{
			name:      "Hostname with underscores",
			hostname:  "example_com",
			wantError: nil,
		},
		{
			name:      "Hostname with hyphens",
			hostname:  "example-com",
			wantError: nil,
		},
		{
			name:      "Hostname with mixed valid characters",
			hostname:  "example-123_com",
			wantError: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateHostName(tt.hostname)
			if tt.wantError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantError.Error())
			}

			asURL := "http://" + tt.hostname
			if tt.isV6Addr {
				asURL = fmt.Sprintf("http://[%s]", tt.hostname)
			}

			err = ValidateHostName(asURL)
			if tt.wantError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantError.Error())
			}
		})
	}
}

// TestLogInvalidHostname checks that an empty hostname logs at debug level and
// every other validation failure stays at error level.
func TestLogInvalidHostname(t *testing.T) {
	tests := map[string]struct {
		err       error
		wantLevel slog.Level
	}{
		"empty hostname": {
			err:       ErrHostnameEmpty,
			wantLevel: slog.LevelDebug,
		},
		"hostname too long": {
			err:       ErrHostnameToLong,
			wantLevel: slog.LevelError,
		},
		"hostname invalid": {
			err:       ErrHostnameInvalid,
			wantLevel: slog.LevelError,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			logger, recorder := logtest.NewWithRecorder(nil)
			ctx := logv2.WithLogger(context.Background(), logger)

			LogInvalidHostname(ctx, tt.err, slog.String("lookup_name", "example.com"))

			records := recorder.Records()
			require.Len(t, records, 1)
			require.Equal(t, "invalid hostname", records[0].Message)
			require.Equal(t, tt.wantLevel, records[0].Level)
			require.Contains(t, records[0].Attrs, slog.String("lookup_name", "example.com"))
		})
	}
}
