package uniquedomain

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
)

type fixedResolver struct {
	request *serving.Request
}

func (r fixedResolver) Resolve(*http.Request) (*serving.Request, error) {
	return r.request, nil
}

func TestMiddlewareLogsUniqueURLWithoutQuery(t *testing.T) {
	buf := loggingtest.Capture(t, slog.LevelInfo)

	r := httptest.NewRequest(http.MethodGet, "https://example.com/project?token=unique-secret#fragment-secret", nil)
	d := domain.New("example.com", "", "", "", 0, fixedResolver{request: &serving.Request{
		LookupPath: &serving.LookupPath{UniqueHost: "unique.example.com"},
	}})
	r = domain.ReqWithDomain(r, d)
	w := httptest.NewRecorder()
	middleware := NewMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	middleware.ServeHTTP(w, r)

	require.Equal(t, "https://unique.example.com/project?token=unique-secret#fragment-secret", w.Header().Get("Location"))
	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), "unique-secret")
	require.NotContains(t, buf.String(), "fragment-secret")
	require.Contains(t, buf.String(), `"host":"example.com"`)
}
