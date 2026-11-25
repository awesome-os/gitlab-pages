package testhelpers

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// Run runs the tests and performs leak detection using goleak.
// It should be called in the TestMain function of test files.
func Run(m *testing.M) {
	VerifyNoGoroutines(m)
}

// VerifyNoGoroutines stops any known global Goroutine handlers and verifies that no
// lingering Goroutines are present.
func VerifyNoGoroutines(m *testing.M) {
	code := m.Run()

	err := goleak.Find(
		// Workaround for https://github.com/patrickmn/go-cache/issues/166#issuecomment-1551983476
		goleak.IgnoreTopFunction("github.com/patrickmn/go-cache.(*janitor).Run"),
	)

	if err != nil {
		log.Fatalf("Found lingering Goroutines: %v\n", err)
	}

	os.Exit(code)
}

// AssertRedirectTo asserts that handler redirects to particular URL
func AssertRedirectTo(t *testing.T, handler http.HandlerFunc, method string,
	url string, values url.Values, expectedURL string) {
	require.HTTPRedirect(t, handler, method, url, values)

	recorder := httptest.NewRecorder()

	req, _ := http.NewRequest(method, url, nil)
	req.URL.RawQuery = values.Encode()

	handler(recorder, req)

	require.Equal(t, expectedURL, recorder.Header().Get("Location"))
}

// AssertLogContains checks that wantLogEntry is contained in at least one of the log entries
func AssertLogContains(t *testing.T, wantLogEntry string, entries []*logrus.Entry) {
	t.Helper()

	if wantLogEntry != "" {
		messages := make([]string, len(entries))
		for k, entry := range entries {
			messages[k] = entry.Message
		}

		require.Contains(t, messages, wantLogEntry)
	}
}

// ToFileProtocol appends the file:// protocol to the current os.Getwd
// and formats path to be a full filepath
func ToFileProtocol(t *testing.T, path string) string {
	t.Helper()

	wd := Getwd(t)

	return fmt.Sprintf("file://%s/%s", wd, path)
}

// Getwd must return current working directory
func Getwd(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)

	return wd
}

// HTTPResponse represents the structure of the HTTP response.
type HTTPResponse struct {
	StatusCode int
	Body       string
	Headers    http.Header
}

// PerformRequest makes an HTTP request and returns the response details.
func PerformRequest(t *testing.T, handler http.Handler, r *http.Request) HTTPResponse {
	t.Helper()

	ww := httptest.NewRecorder()

	handler.ServeHTTP(ww, r)
	res := ww.Result()

	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	return HTTPResponse{
		StatusCode: res.StatusCode,
		Body:       string(b),
		Headers:    res.Header,
	}
}

// Close will call the close function on a closer as part
// of the t.Cleanup function.
func Close(t *testing.T, c io.Closer) {
	t.Helper()

	t.Cleanup(func() {
		require.NoError(t, c.Close())
	})
}

// CertPool creates a new certificate pool containing the certificate.
func CertPool(tb testing.TB, certPath string) *x509.CertPool {
	tb.Helper()
	pem := MustReadFile(tb, certPath)
	pool := x509.NewCertPool()
	require.True(tb, pool.AppendCertsFromPEM(pem))
	return pool
}

// Cert returns the parsed certificate.
func Cert(tb testing.TB, certPath, keyPath string) tls.Certificate {
	tb.Helper()
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	require.NoError(tb, err)
	return cert
}

// MustReadFile returns the content of a file or fails at once.
func MustReadFile(tb testing.TB, filename string) []byte {
	tb.Helper()

	content, err := os.ReadFile(filename)
	if err != nil {
		tb.Fatal(err)
	}

	return content
}

func Sha(path string) string {
	sha := sha256.Sum256([]byte(path))
	s := hex.EncodeToString(sha[:])
	return s
}

// ServeZipFile serves the zip file content with a mock HTTP server
func ServeZipFile(content []byte, handlerURL string) *httptest.Server {
	modtime := time.Now()
	m := http.NewServeMux()
	m.HandleFunc(handlerURL, func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "public.zip", modtime, bytes.NewReader(content))
	})
	return httptest.NewServer(m)
}
