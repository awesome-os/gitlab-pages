package acceptance_test

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentVariablesConfig(t *testing.T) {
	// This test configures the listener through an environment variable rather
	// than withListeners, so it allocates the port itself instead of relying on
	// RunPagesProcess to assign one. Use a local copy so we do not mutate the
	// shared package-global httpListener, which would leak into later tests.
	spec := httpListener
	spec.Port = allocateFreePort(t, spec.Host)
	t.Setenv("LISTEN_HTTP", net.JoinHostPort(spec.Host, spec.Port))

	RunPagesProcess(t,
		withoutWait,
		withListeners([]ListenSpec{}), // explicitly disable listeners for this test
	)
	require.NoError(t, spec.WaitUntilRequestSucceeds(nil))

	rsp, err := GetPageFromListener(t, spec, "group.gitlab-example.com", "project/")

	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	require.NotNil(t, rsp.Header.Get("X-Request-ID"))
}

func TestMixedConfigSources(t *testing.T) {
	// The HTTP listener is configured through an environment variable, so its
	// port is allocated here into a local copy to avoid mutating the shared
	// package-global httpListener; the HTTPS listener goes through withListeners
	// and is assigned a port by RunPagesProcess (which updates the global
	// httpsListener).
	spec := httpListener
	spec.Port = allocateFreePort(t, spec.Host)
	t.Setenv("LISTEN_HTTP", net.JoinHostPort(spec.Host, spec.Port))

	RunPagesProcess(t,
		withoutWait,
		withListeners([]ListenSpec{httpsListener}),
	)

	for _, listener := range []ListenSpec{spec, httpsListener} {
		require.NoError(t, listener.WaitUntilRequestSucceeds(nil))
		rsp, err := GetPageFromListener(t, listener, "group.gitlab-example.com", "project/")
		require.NoError(t, err)
		rsp.Body.Close()

		require.Equal(t, http.StatusOK, rsp.StatusCode)
	}
}

func TestMultipleListenersFromEnvironmentVariables(t *testing.T) {
	port1 := allocateFreePort(t, "127.0.0.1")
	port2 := allocateFreePort(t, "127.0.0.1")
	listenSpecs := []ListenSpec{{"http", "127.0.0.1", port1, false}, {"http", "127.0.0.1", port2, false}}
	t.Setenv("LISTEN_HTTP", fmt.Sprintf("%s,%s", net.JoinHostPort("127.0.0.1", port1), net.JoinHostPort("127.0.0.1", port2)))

	RunPagesProcess(t,
		withoutWait,
		withListeners([]ListenSpec{}), // explicitly disable listeners for this test
	)

	for _, listener := range listenSpecs {
		require.NoError(t, listener.WaitUntilRequestSucceeds(nil))
		rsp, err := GetPageFromListener(t, listener, "group.gitlab-example.com", "project/")

		require.NoError(t, err)
		rsp.Body.Close()
		require.Equal(t, http.StatusOK, rsp.StatusCode)
	}
}

func TestUnixSocketListener(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "unix.sock")

	spec := ListenSpec{
		Type: "unix",
		Host: sockPath,
	}

	RunPagesProcess(t,
		withListeners([]ListenSpec{spec}),
	)
	require.NoError(t, spec.WaitUntilRequestSucceeds(nil))

	rsp, err := GetPageFromListener(t, spec, "group.gitlab-example.com", "project/")

	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusOK, rsp.StatusCode)
}
