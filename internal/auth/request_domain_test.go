package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetRequestDomain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		host      string
		namespace string
		want      string
	}{
		{
			name:      "wildcard layout: namespace subdomain moves into the path",
			host:      "ns.pages.example.com",
			namespace: "ns",
			want:      "https://pages.example.com/ns",
		},
		{
			name:      "no namespace",
			host:      "pages.example.com",
			namespace: "",
			want:      "https://pages.example.com",
		},
		{
			name:      "namespace equals the host",
			host:      "pages.acceptance.example.com",
			namespace: "pages.acceptance.example.com",
			want:      "https://pages.acceptance.example.com",
		},
		{
			name:      "namespace is not part of the host",
			host:      "foobar.example.com",
			namespace: "foo",
			want:      "https://foobar.example.com",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "https://"+tc.host+"/", nil)
			r.Host = tc.host
			require.Equal(t, tc.want, getRequestDomain(r, tc.namespace))
		})
	}
}
