package domain_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/confighelper"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain/mock"
	"gitlab.com/gitlab-org/gitlab-pages/internal/fixture"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	servZip "gitlab.com/gitlab-org/gitlab-pages/internal/serving/disk/zip"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func serveFileOrNotFound(domain *domain.Domain) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !domain.ServeFileHTTP(w, r) {
			domain.ServeNotFoundHTTP(w, r)
		}
	}
}

// zipServingInstance returns a new instance of the zip serving implementation
// configured with the default test configuration. It is used in tests to create
// a new zip serving instance.
func zipServingInstance(t *testing.T) serving.Serving {
	t.Helper()

	zipServing := servZip.NewInstance()

	cfg := &config.Config{
		Zip: confighelper.DefaultZipCfgForTest,
	}
	err := zipServing.Reconfigure(cfg)
	require.NoError(t, err)

	return zipServing
}

func TestIsHTTPSOnly(t *testing.T) {
	servingInstance := zipServingInstance(t)
	defer servingInstance.Stop()

	tests := []struct {
		name     string
		domain   *domain.Domain
		url      string
		expected bool
	}{
		{
			name: "Custom domain with HTTPS-only enabled",
			domain: domain.New("custom-domain", "", "", "", 1,
				mockResolver(t,
					&serving.LookupPath{
						Path:          "group/project/public",
						SHA256:        "foo",
						IsHTTPSOnly:   true,
						RootDirectory: "public",
					},
					servingInstance,
					"",
					nil)),
			url:      "http://custom-domain",
			expected: true,
		},
		{
			name: "Custom domain with HTTPS-only disabled",
			domain: domain.New("custom-domain", "", "", "", 1,
				mockResolver(t,
					&serving.LookupPath{
						Path:          "group/project/public",
						SHA256:        "foo",
						IsHTTPSOnly:   false,
						RootDirectory: "public",
					},
					servingInstance,
					"",
					nil)),
			url:      "http://custom-domain",
			expected: false,
		},
		{
			name:     "Unknown project",
			domain:   domain.New("", "", "", "", 1, mockResolver(t, nil, servingInstance, "", domain.ErrDomainDoesNotExist)),
			url:      "http://test-domain/project",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, test.url, nil)
			require.Equal(t, test.expected, test.domain.IsHTTPSOnly(req))
		})
	}
}

func TestPredefined404ServeHTTP(t *testing.T) {
	servingInstance := zipServingInstance(t)
	defer servingInstance.Stop()

	testDomain := domain.New("", "", "", "", 1, mockResolver(t, nil, servingInstance, "", domain.ErrDomainDoesNotExist))

	require.HTTPStatusCode(t, serveFileOrNotFound(testDomain), http.MethodGet, "http://group.test.io/not-existing-file", nil, http.StatusNotFound)
	require.HTTPBodyContains(t, serveFileOrNotFound(testDomain), http.MethodGet, "http://group.test.io/not-existing-file", nil, "The page you're looking for could not be found")
}

func TestGroupCertificate(t *testing.T) {
	testGroup := &domain.Domain{}

	tls, err := testGroup.EnsureCertificate()
	require.Nil(t, tls)
	require.Error(t, err)
}

func TestDomainNoCertificate(t *testing.T) {
	testDomain := &domain.Domain{
		Name: "test.domain.com",
	}

	tls, err := testDomain.EnsureCertificate()
	require.Nil(t, tls)
	require.Error(t, err)

	_, err2 := testDomain.EnsureCertificate()
	require.Error(t, err)
	require.Equal(t, err, err2)
}

func TestDomainValidCertificate(t *testing.T) {
	d := &domain.Domain{
		Name:            "valid.domain.com",
		CertificateCert: fixture.Certificate,
		CertificateKey:  fixture.Key,
	}
	cert, err := d.EnsureCertificate()
	require.NoError(t, err)
	require.NotNil(t, cert)
}

func TestDomainInvalidCertificate(t *testing.T) {
	d := &domain.Domain{
		Name:            "invalid.domain.com",
		CertificateCert: "invalid-cert",
		CertificateKey:  "invalid-key",
	}
	cert, err := d.EnsureCertificate()
	require.Error(t, err)
	require.Nil(t, cert)
}

func BenchmarkEnsureCertificate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testDomain := &domain.Domain{
			Name:            "test.domain.com",
			CertificateCert: fixture.Certificate,
			CertificateKey:  fixture.Key,
		}

		testDomain.EnsureCertificate()
	}
}

var chdirSet = false

func newZipFileServerURL(t *testing.T, zipFilePath string, requests *int64) string {
	t.Helper()

	chdir := testhelpers.ChdirInPath(t, "../../shared/pages", &chdirSet)

	m := http.NewServeMux()
	m.HandleFunc("/public.zip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			atomic.AddInt64(requests, 1)
		}

		r.ParseForm()

		if changedContent := r.Form.Get("changed-content"); changedContent != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		http.ServeFile(w, r, zipFilePath)
	}))

	testServer := httptest.NewServer(m)

	t.Cleanup(func() {
		chdir()
		testServer.Close()
	})

	return testServer.URL
}

func TestServeNamespaceNotFound(t *testing.T) {
	testServerURL := newZipFileServerURL(t, "group.404/group.404.gitlab-example.com/public.zip", nil)

	path := testServerURL + "/public.zip"
	pathSha := testhelpers.Sha(path)

	servingInstance := zipServingInstance(t)
	defer servingInstance.Stop()

	tests := []struct {
		name             string
		domain           string
		path             string
		resolver         domain.Resolver
		expectedResponse string
	}{
		{
			name:   "public_namespace_domain",
			domain: "group.404.gitlab-example.com",
			path:   "/unknown",
			resolver: mockResolver(t,
				&serving.LookupPath{
					Path:               path,
					SHA256:             pathSha,
					IsNamespaceProject: true,
					RootDirectory:      "public",
				},
				servingInstance,
				"/unknown",
				nil,
			),
			expectedResponse: "Custom 404 group page",
		},
		{
			name:   "private_project_under_public_namespace_domain",
			domain: "group.404.gitlab-example.com",
			path:   "/private_project/unknown",
			resolver: mockResolver(t,
				&serving.LookupPath{
					Path:               path,
					SHA256:             pathSha,
					IsNamespaceProject: true,
					HasAccessControl:   false,
					RootDirectory:      "public",
				},
				servingInstance,
				"/",
				nil,
			),
			expectedResponse: "Custom 404 group page",
		},
		{
			name:   "private_namespace_domain",
			domain: "group.404.gitlab-example.com",
			path:   "/unknown",
			resolver: mockResolver(t,
				&serving.LookupPath{
					Path:               path,
					SHA256:             pathSha,
					IsNamespaceProject: true,
					HasAccessControl:   true,
					RootDirectory:      "public",
				},
				servingInstance,
				"/",
				nil,
			),
			expectedResponse: "The page you're looking for could not be found.",
		},
		{
			name:   "no_parent_namespace_domain",
			domain: "group.404.gitlab-example.com",
			path:   "/unknown",
			resolver: mockResolver(t,
				nil,
				servingInstance,
				"/",
				domain.ErrDomainDoesNotExist,
			),
			expectedResponse: "The page you're looking for could not be found.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &domain.Domain{
				Name:     tt.domain,
				Resolver: tt.resolver,
			}

			require.HTTPStatusCode(t, d.ServeNamespaceNotFound, http.MethodGet, fmt.Sprintf("http://%s%s", tt.domain, tt.path), nil, http.StatusNotFound)
			require.HTTPBodyContains(t, d.ServeNamespaceNotFound, http.MethodGet, fmt.Sprintf("http://%s%s", tt.domain, tt.path), nil, tt.expectedResponse)
		})
	}
}

func TestDomainGetRootNamespaceID(t *testing.T) {
	testCases := []struct {
		name          string
		domain        *domain.Domain
		expectedID    int
		expectedError error
	}{
		{
			name: "returns root namespace ID when domain exists",
			domain: &domain.Domain{
				RootNamespaceID: 12345,
			},
			expectedID:    12345,
			expectedError: nil,
		},
		{
			name:          "returns zero and error when domain is nil",
			domain:        nil,
			expectedID:    0,
			expectedError: domain.ErrDomainDoesNotExist,
		},
		{
			name: "returns zero when RootNamespaceID is zero",
			domain: &domain.Domain{
				RootNamespaceID: 0,
			},
			expectedID:    0,
			expectedError: nil,
		},
		{
			name: "returns negative ID when RootNamespaceID is negative",
			domain: &domain.Domain{
				RootNamespaceID: -1,
			},
			expectedID:    -1,
			expectedError: nil,
		},
		{
			name: "returns large namespace ID",
			domain: &domain.Domain{
				RootNamespaceID: 999999999,
			},
			expectedID:    999999999,
			expectedError: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := tc.domain.GetRootNamespaceID()

			require.Equal(t, tc.expectedID, id)

			if tc.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, tc.expectedError, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func mockResolver(t *testing.T, project *serving.LookupPath, zipServing serving.Serving, subpath string, err error) domain.Resolver {
	mockCtrl := gomock.NewController(t)

	mockResolver := mock.NewMockResolver(mockCtrl)

	mockResolver.EXPECT().
		Resolve(gomock.Any()).
		AnyTimes().
		Return(&serving.Request{
			Serving:    zipServing,
			LookupPath: project,
			SubPath:    subpath,
		}, err)

	return mockResolver
}
