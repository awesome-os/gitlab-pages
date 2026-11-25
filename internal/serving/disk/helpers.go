package disk

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	contentencoding "gitlab.com/feistel/go-contentencoding/encoding"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
)

var (
	// Server side content encoding priority.
	supportedEncodings = contentencoding.Preference{
		contentencoding.Brotli:   1.0,
		contentencoding.Gzip:     0.5,
		contentencoding.Identity: 0.1,
	}

	compressedEncodings = map[string]string{
		contentencoding.Brotli: ".br",
		contentencoding.Gzip:   ".gz",
	}

	mimeTypes = map[string]string{
		extTxt:    mimeTextPlainUTF8,
		extRDF:    mimeRdfXML,
		extTTL:    mimeTurtleUTF8,
		extN3:     mimeN3UTF8,
		extNT:     mimeNTriples,
		extJSONLD: mimeLdJSON,
		extOwl:    mimeRdfXML,
		extOWX:    mimeOwlXML,
	}
)

func endsWithSlash(path string) bool {
	return strings.HasSuffix(path, "/")
}

func endsWithoutHTMLExtension(path string) bool {
	return !strings.HasSuffix(path, ".html")
}

func cloneURL(originalURL *url.URL) *url.URL {
	newURL := new(url.URL)

	// Copy relevant fields
	*newURL = *originalURL

	return newURL
}

// Detect file's content-type either by extension or mime-sniffing.
// Implementation is adapted from Golang's `http.serveContent()`
// See https://github.com/golang/go/blob/902fc114272978a40d2e65c2510a18e870077559/src/net/http/fs.go#L194
func detectContentType(ctx context.Context, root vfs.Root, path string) (string, error) {
	ext := filepath.Ext(path)

	if contentType := mime.TypeByExtension(ext); contentType != "" {
		return contentType, nil
	}

	if contentType := detectContentTypeByExtension(ext); contentType != "" {
		return contentType, nil
	}

	return detectContentTypeByReading(ctx, root, path)
}

func detectContentTypeByExtension(ext string) string {
	if !feature.ContentNegotiation.Enabled() {
		if ext == extTxt {
			return mimeTextPlainUTF8
		}
		return ""
	}
	return mimeTypes[ext]
}

func detectContentTypeByReading(ctx context.Context, root vfs.Root, path string) (string, error) {
	file, err := root.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var buf [512]byte
	n, _ := io.ReadFull(file, buf[:])
	return http.DetectContentType(buf[:n]), nil
}

// getPathWithEncoding returns the path to serve and the negotiated encoding
// ("" for identity). It does not modify the response; callers set Vary,
// Content-Encoding and Content-Length.
func getPathWithEncoding(ctx context.Context, r *http.Request, root vfs.Root, fullPath string) (path, encoding string) {
	// don't accept range requests for compressed content
	if r.Header.Get("Range") != "" {
		return fullPath, ""
	}

	acceptHeader := r.Header.Get("Accept-Encoding")

	// don't send compressed content if there's no accept-encoding header
	if acceptHeader == "" {
		return fullPath, ""
	}

	results, err := supportedEncodings.Negotiate(acceptHeader, contentencoding.AliasIdentity)
	if err != nil {
		return fullPath, ""
	}

	if len(results) == 0 {
		return fullPath, ""
	}

	for _, candidate := range results {
		if candidate == contentencoding.Identity {
			break
		}

		extension := compressedEncodings[candidate]
		candidatePath := fullPath + extension

		// Ensure the file is not a symlink
		if fi, err := root.Lstat(ctx, candidatePath); err == nil && fi.Mode().IsRegular() {
			return candidatePath, candidate
		}
	}

	return fullPath, ""
}
