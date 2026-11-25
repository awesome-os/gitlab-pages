package redirects

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/confighelper"
	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs/zip"
)

func generateRedirects(count int) string {
	content := "/start.html /redirect.html 301\n"
	if feature.RedirectsPlaceholders.Enabled() {
		content += strings.Repeat("/foo/*/bar /foo/:splat/qux 200\n", count/2)
		content += strings.Repeat("/foo/:placeholder /qux/:placeholder 200\n", count/2)
	} else {
		content += strings.Repeat("/goto.html /target.html 301\n", count)
	}

	content += "/entrance.html /exit.html 301\n"

	return content
}

func benchmarkRedirectsRewrite(b *testing.B, redirectsCount int) {
	// Create a zip file with _redirects file populated based on redirectsCount
	zipContent, err := generateZipContent(generateRedirects(redirectsCount))
	require.NoError(b, err)

	// Serve the zip file with a mock HTTP server
	server := testhelpers.ServeZipFile(zipContent, "/public.zip")
	defer server.Close()

	defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
	defer defaultZipVFS.Stop()

	// Create a VFS root from the served zip file
	vfsRoot, err := createVFSRoot(server.URL+"/public.zip", defaultZipVFS)
	require.NoError(b, err)

	// Parse redirects from the VFS root
	redirects, err := parseRedirectsFromVFS(context.Background(), vfsRoot)
	require.NoError(b, err)

	// Benchmark the Rewrite function
	url, _ := url.Parse("/entrance.html")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err = redirects.Rewrite(context.Background(), url, "correlationId")
		require.NoError(b, err)
	}
}

func BenchmarkRedirectsRewrite_withoutPlaceholders(b *testing.B) {
	b.Run("10 redirects", func(b *testing.B) { benchmarkRedirectsRewrite(b, 10) })
	b.Run("100 redirects", func(b *testing.B) { benchmarkRedirectsRewrite(b, 100) })
	b.Run("1000 redirects", func(b *testing.B) { benchmarkRedirectsRewrite(b, 998) })
}

func BenchmarkRedirectsRewrite_PlaceholdersEnabled(b *testing.B) {
	b.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	b.Run("10 redirects", func(b *testing.B) { benchmarkRedirectsRewrite(b, 10) })
	b.Run("100 redirects", func(b *testing.B) { benchmarkRedirectsRewrite(b, 100) })
	b.Run("1000 redirects", func(b *testing.B) { benchmarkRedirectsRewrite(b, 998) })
}

func benchmarkRedirectsParseRedirects(b *testing.B, redirectsCount int) {
	// Create a zip file with _redirects file populated based on redirectsCount
	zipContent, err := generateZipContent(generateRedirects(redirectsCount))
	require.NoError(b, err)

	// Serve the zip file with a mock HTTP server
	server := testhelpers.ServeZipFile(zipContent, "/public.zip")
	defer server.Close()

	defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
	defer defaultZipVFS.Stop()

	// Create a VFS root from the served zip file
	vfsRoot, err := createVFSRoot(server.URL+"/public.zip", defaultZipVFS)
	require.NoError(b, err)

	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		// Parse redirects from the VFS root
		_, err = parseRedirectsFromVFS(ctx, vfsRoot)
		require.NoError(b, err)
	}
}

func BenchmarkRedirectsParseRedirects(b *testing.B) {
	b.Run("10 redirects", func(b *testing.B) { benchmarkRedirectsParseRedirects(b, 10) })
	b.Run("100 redirects", func(b *testing.B) { benchmarkRedirectsParseRedirects(b, 100) })
	b.Run("1000 redirects", func(b *testing.B) { benchmarkRedirectsParseRedirects(b, 998) })
}
