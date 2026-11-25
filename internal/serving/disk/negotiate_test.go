package disk

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
)

func TestParseAcceptHeader(t *testing.T) {
	tests := map[string]struct {
		header   string
		expected []mimeCandidate
	}{
		"empty header": {
			header:   "",
			expected: nil,
		},
		"single media type without quality": {
			header: "application/rdf+xml",
			expected: []mimeCandidate{
				{mediaType: "application/rdf+xml", quality: 1.0, specificity: 2, order: 0},
			},
		},
		"multiple media types with quality weights": {
			header: "text/html;q=0.5, application/rdf+xml;q=0.9, text/turtle;q=1.0",
			expected: []mimeCandidate{
				{mediaType: "text/turtle", quality: 1.0, specificity: 2, order: 2},
				{mediaType: "application/rdf+xml", quality: 0.9, specificity: 2, order: 1},
				{mediaType: "text/html", quality: 0.5, specificity: 2, order: 0},
			},
		},
		"quality weighting and specificity ordering": {
			header: "*/*;q=0.8, text/*;q=0.8, text/html;q=0.8",
			expected: []mimeCandidate{
				{mediaType: "text/html", quality: 0.8, specificity: 2, order: 2},
				{mediaType: "text/*", quality: 0.8, specificity: 1, order: 1},
				{mediaType: "*/*", quality: 0.8, specificity: 0, order: 0},
			},
		},
		"zero quality is excluded": {
			header: "application/rdf+xml, text/html;q=0",
			expected: []mimeCandidate{
				{mediaType: "application/rdf+xml", quality: 1.0, specificity: 2, order: 0},
			},
		},
		"out of range or invalid quality values are excluded": {
			header: "t/1;q=NaN, t/2;q=Inf, t/3;q=1.5, t/4;q=-0.1, t/5;q=0.8",
			expected: []mimeCandidate{
				{mediaType: "t/5", quality: 0.8, specificity: 2, order: 4},
			},
		},
		"malformed entry with q=0 is excluded rather than defaulted to 1": {
			header: "text/html; malformed; q=0, text/turtle",
			expected: []mimeCandidate{
				{mediaType: "text/turtle", quality: 1.0, specificity: 2, order: 1},
			},
		},
		"duplicate media types are deduplicated": {
			header: "text/html, text/html;q=0.9, text/html;q=0.8",
			expected: []mimeCandidate{
				{mediaType: "text/html", quality: 1.0, specificity: 2, order: 0},
			},
		},
		"bounded by parts inspected rather than appended candidates": {
			header: "t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t1/a, t2/b",
			expected: []mimeCandidate{
				{mediaType: "t1/a", quality: 1.0, specificity: 2, order: 0},
			},
		},
		"capped at maxAcceptParts distinct candidates": {
			header: "t1/a, t2/b, t3/c, t4/d, t5/e, t6/f, t7/g, t8/h, t9/i, t10/j, t11/k, t12/l, t13/m, t14/n, t15/o, t16/p, t17/q, t18/r",
			expected: []mimeCandidate{
				{mediaType: "t1/a", quality: 1.0, specificity: 2, order: 0},
				{mediaType: "t2/b", quality: 1.0, specificity: 2, order: 1},
				{mediaType: "t3/c", quality: 1.0, specificity: 2, order: 2},
				{mediaType: "t4/d", quality: 1.0, specificity: 2, order: 3},
				{mediaType: "t5/e", quality: 1.0, specificity: 2, order: 4},
				{mediaType: "t6/f", quality: 1.0, specificity: 2, order: 5},
				{mediaType: "t7/g", quality: 1.0, specificity: 2, order: 6},
				{mediaType: "t8/h", quality: 1.0, specificity: 2, order: 7},
				{mediaType: "t9/i", quality: 1.0, specificity: 2, order: 8},
				{mediaType: "t10/j", quality: 1.0, specificity: 2, order: 9},
				{mediaType: "t11/k", quality: 1.0, specificity: 2, order: 10},
				{mediaType: "t12/l", quality: 1.0, specificity: 2, order: 11},
				{mediaType: "t13/m", quality: 1.0, specificity: 2, order: 12},
				{mediaType: "t14/n", quality: 1.0, specificity: 2, order: 13},
				{mediaType: "t15/o", quality: 1.0, specificity: 2, order: 14},
				{mediaType: "t16/p", quality: 1.0, specificity: 2, order: 15},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseAcceptHeader(tc.header)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestReader_ContentNegotiation(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "true")

	files := map[string][]byte{
		"index.html":     []byte("<html>index</html>"),
		"index.rdf":      []byte("<rdf:RDF>index</rdf:RDF>"),
		"page.html":      []byte("<html>page</html>"),
		"page.rdf":       []byte("<rdf:RDF>data</rdf:RDF>"),
		"page.ttl":       []byte("@prefix : <http://example.org/> ."),
		"page.jsonld":    []byte(`{"@context": "http://schema.org"}`),
		"ontology.owl":   []byte("<rdf:RDF>ontology</rdf:RDF>"),
		"ontology.owx":   []byte("<Ontology>owl</Ontology>"),
		"only_html.html": []byte("<html>only html</html>"),
	}

	tests := map[string]struct {
		urlPath             string
		acceptHeader        string
		expectedBody        string
		expectedContentType string
		expectedVaryAccept  bool
		expectedETag        string
	}{
		"request /page with application/rdf+xml returns page.rdf": {
			urlPath:             "/page",
			acceptHeader:        "application/rdf+xml",
			expectedBody:        "<rdf:RDF>data</rdf:RDF>",
			expectedContentType: "application/rdf+xml",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.rdf"`,
		},
		"request /page with text/turtle returns page.ttl": {
			urlPath:             "/page",
			acceptHeader:        "text/turtle",
			expectedBody:        "@prefix : <http://example.org/> .",
			expectedContentType: "text/turtle; charset=utf-8",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.ttl"`,
		},
		"request /page with application/ld+json returns page.jsonld": {
			urlPath:             "/page",
			acceptHeader:        "application/ld+json",
			expectedBody:        `{"@context": "http://schema.org"}`,
			expectedContentType: "application/ld+json",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.jsonld"`,
		},
		"request /ontology with application/owl+xml returns ontology.owx": {
			urlPath:             "/ontology",
			acceptHeader:        "application/owl+xml",
			expectedBody:        "<Ontology>owl</Ontology>",
			expectedContentType: "application/owl+xml",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.owx"`,
		},
		"request /ontology with application/rdf+xml returns ontology.owl": {
			urlPath:             "/ontology",
			acceptHeader:        "application/rdf+xml",
			expectedBody:        "<rdf:RDF>ontology</rdf:RDF>",
			expectedContentType: "application/rdf+xml",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.owl"`,
		},
		"request /page with quality preference prefers turtle over rdf": {
			urlPath:             "/page",
			acceptHeader:        "application/rdf+xml;q=0.5, text/turtle;q=0.9",
			expectedBody:        "@prefix : <http://example.org/> .",
			expectedContentType: "text/turtle; charset=utf-8",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.ttl"`,
		},
		"request /only_html with application/rdf+xml falls back to only_html.html": {
			urlPath:             "/only_html",
			acceptHeader:        "application/rdf+xml",
			expectedBody:        "<html>only html</html>",
			expectedContentType: "text/html; charset=utf-8",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.html"`,
		},
		"request /page without Accept header returns page.html with Vary Accept": {
			urlPath:             "/page",
			acceptHeader:        "",
			expectedBody:        "<html>page</html>",
			expectedContentType: "text/html; charset=utf-8",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.html"`,
		},
		"request / with Accept application/rdf+xml returns index.rdf": {
			urlPath:             "/",
			acceptHeader:        "application/rdf+xml",
			expectedBody:        "<rdf:RDF>index</rdf:RDF>",
			expectedContentType: "application/rdf+xml",
			expectedVaryAccept:  true,
			expectedETag:        `"sha.rdf"`,
		},
		"direct request to /page.rdf returns page.rdf without Vary Accept": {
			urlPath:             "/page.rdf",
			acceptHeader:        "text/html",
			expectedBody:        "<rdf:RDF>data</rdf:RDF>",
			expectedContentType: "application/rdf+xml",
			expectedVaryAccept:  false,
			expectedETag:        `"sha"`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newDiskReaderWithFiles(t, dirRoot{}, files)

			req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io"+tc.urlPath, nil)
			if tc.acceptHeader != "" {
				req.Header.Set("Accept", tc.acceptHeader)
			}
			w := httptest.NewRecorder()

			subPath := tc.urlPath[1:]
			if subPath == "" {
				subPath = "/"
			}

			handled := reader.tryFile(serving.Handler{
				Writer:     w,
				Request:    req,
				LookupPath: &serving.LookupPath{SHA256: "sha"},
				SubPath:    subPath,
			})

			require.True(t, handled)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tc.expectedBody, w.Body.String())
			require.Equal(t, tc.expectedContentType, w.Header().Get("Content-Type"))
			require.Equal(t, tc.expectedETag, w.Header().Get("ETag"))

			varyHeaders := w.Header().Values("Vary")
			if tc.expectedVaryAccept {
				require.Contains(t, varyHeaders, "Accept")
			} else {
				require.NotContains(t, varyHeaders, "Accept")
			}
		})
	}
}

func TestReader_ContentNegotiation_FeatureFlagDisabled(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "false")

	files := map[string][]byte{
		"page.html": []byte("<html>page</html>"),
		"page.ttl":  []byte("@prefix : <http://example.org/> ."),
	}

	reader := newDiskReaderWithFiles(t, dirRoot{}, files)

	req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/page", nil)
	req.Header.Set("Accept", "text/turtle")
	w := httptest.NewRecorder()

	handled := reader.tryFile(serving.Handler{
		Writer:     w,
		Request:    req,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "page",
	})

	require.True(t, handled)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "<html>page</html>", w.Body.String())
	require.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	require.NotContains(t, w.Header().Values("Vary"), "Accept")
}

func TestReader_ContentNegotiation_BrowserAcceptDoesNotServeNonSemanticFiles(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "true")

	// Site with feed.xml and logo.webp, but NO feed.html or logo.html
	files := map[string][]byte{
		"feed.xml":  []byte("<rss>feed</rss>"),
		"logo.webp": []byte("RIFF...WEBP"),
	}

	reader := newDiskReaderWithFiles(t, dirRoot{}, files)

	browserAccept := "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"

	for _, path := range []string{"/feed", "/logo"} {
		req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io"+path, nil)
		req.Header.Set("Accept", browserAccept)
		w := httptest.NewRecorder()

		handled := reader.tryFile(serving.Handler{
			Writer:     w,
			Request:    req,
			LookupPath: &serving.LookupPath{SHA256: "sha"},
			SubPath:    path[1:],
		})

		// tryFile must return false so that serving pipeline can fall through
		// to _redirects rules or custom 404.html without prematurely serving feed.xml or logo.webp.
		require.False(t, handled)
		require.NotContains(t, w.Header().Values("Vary"), "Accept")
	}
}

func TestReader_ContentNegotiation_MissingPath_NoVaryAccept(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "true")

	reader := newDiskReaderWithFiles(t, dirRoot{}, map[string][]byte{
		"page.html": []byte("<html>page</html>"),
	})

	req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/nonexistent", nil)
	req.Header.Set("Accept", "text/turtle, text/html")
	w := httptest.NewRecorder()

	handled := reader.tryFile(serving.Handler{
		Writer:     w,
		Request:    req,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "nonexistent",
	})

	require.False(t, handled)
	require.NotContains(t, w.Header().Values("Vary"), "Accept")
}

func TestReader_ContentNegotiation_DuplicateVaryNotAdded(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "true")

	reader := newDiskReaderWithFiles(t, dirRoot{}, map[string][]byte{
		"page.html": []byte("<html>page</html>"),
	})

	req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/page", nil)
	req.Header.Set("Accept", "text/turtle, text/html")
	w := httptest.NewRecorder()
	w.Header().Add("Vary", "Accept")

	handled := reader.tryFile(serving.Handler{
		Writer:     w,
		Request:    req,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "page",
	})

	require.True(t, handled)
	require.ElementsMatch(t, []string{"Accept", "Accept-Encoding"}, w.Header().Values("Vary"))
}

func TestReader_ContentNegotiation_DistinctETags(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "true")

	files := map[string][]byte{
		"page.html": []byte("<html>page</html>"),
		"page.ttl":  []byte("@prefix : <http://example.org/> ."),
	}

	reader := newDiskReaderWithFiles(t, dirRoot{}, files)

	// Request Turtle variant
	req1 := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/page", nil)
	req1.Header.Set("Accept", "text/turtle")
	w1 := httptest.NewRecorder()
	handled1 := reader.tryFile(serving.Handler{
		Writer:     w1,
		Request:    req1,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "page",
	})
	require.True(t, handled1)
	require.Equal(t, `"sha.ttl"`, w1.Header().Get("ETag"))

	// Request HTML variant
	req2 := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/page", nil)
	req2.Header.Set("Accept", "text/html")
	w2 := httptest.NewRecorder()
	handled2 := reader.tryFile(serving.Handler{
		Writer:     w2,
		Request:    req2,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "page",
	})
	require.True(t, handled2)
	require.Equal(t, `"sha.html"`, w2.Header().Get("ETag"))

	// Representations must carry distinct strong ETags per RFC 9110 §8.8.3
	require.NotEqual(t, w1.Header().Get("ETag"), w2.Header().Get("ETag"))

	// Non-negotiated direct file request retains unadorned deployment SHA ETag
	req3 := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/page.html", nil)
	w3 := httptest.NewRecorder()
	handled3 := reader.tryFile(serving.Handler{
		Writer:     w3,
		Request:    req3,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "page.html",
	})
	require.True(t, handled3)
	require.Equal(t, `"sha"`, w3.Header().Get("ETag"))
}

func TestReader_ContentNegotiation_FeatureFlagDisabled_MimeTypesNotChanged(t *testing.T) {
	t.Setenv("FF_ENABLE_CONTENT_NEGOTIATION", "false")

	require.Equal(t, "text/plain; charset=UTF-8", detectContentTypeByExtension(".txt"))
	require.Empty(t, detectContentTypeByExtension(".rdf"))
	require.Empty(t, detectContentTypeByExtension(".ttl"))
	require.Empty(t, detectContentTypeByExtension(".n3"))
	require.Empty(t, detectContentTypeByExtension(".nt"))
	require.Empty(t, detectContentTypeByExtension(".jsonld"))
	require.Empty(t, detectContentTypeByExtension(".owl"))
	require.Empty(t, detectContentTypeByExtension(".owx"))
}
