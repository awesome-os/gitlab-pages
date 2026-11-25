package logging

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemovingAllSensitiveData(t *testing.T) {
	url := CleanURL("https://user:password@gitlab.com/gitlab?key=value#fragment")
	require.Equal(t, "https://gitlab.com/gitlab", url)
}

func TestRemovingEmptyQuery(t *testing.T) {
	require.Equal(t, "/gitlab", CleanURL("/gitlab?"))
}

func TestInvalidURL(t *testing.T) {
	require.Empty(t, CleanURL("://invalid URL"))
}

func TestCleanURLError(t *testing.T) {
	cause := errors.New("request failed")
	original := &url.Error{
		Op:  "Get",
		URL: "https://user:password@example.com/archive.zip?token=token-secret#fragment-secret",
		Err: cause,
	}

	got := CleanURLError(original)

	require.EqualError(t, got, `Get "https://example.com/archive.zip": request failed`)
	require.ErrorIs(t, got, cause)
	require.Equal(t, "https://user:password@example.com/archive.zip?token=token-secret#fragment-secret", original.URL)
}

func TestCleanURLErrorReturnsOtherErrorsUnchanged(t *testing.T) {
	err := errors.New("request failed")
	require.Same(t, err, CleanURLError(err))
}

func TestCleanURLErrorPreservesWrapper(t *testing.T) {
	cause := errors.New("request failed")
	urlErr := CleanURLError(&url.Error{
		Op:  "Get",
		URL: "https://user:password@example.com/archive.zip?token=token-secret#fragment-secret",
		Err: cause,
	})
	wrapped := fmt.Errorf("read failed: %w", urlErr)

	got := CleanURLError(wrapped)

	require.Same(t, wrapped, got)
	require.EqualError(t, got, `read failed: Get "https://example.com/archive.zip": request failed`)
	require.ErrorIs(t, got, cause)
	var gotURLError *url.Error
	require.ErrorAs(t, got, &gotURLError)
	require.Equal(t, "https://example.com/archive.zip", gotURLError.URL)
}
