package casv11

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// B8GAbiMagicV3 is the canonical B8G ABI v3 magic constant ("BGV3", 0x33564742).
const B8GAbiMagicV3 = uint32(0x33564742)

const (
	casHeaderName  = "X-B8G-CAS"
	casHeaderValue = "v11"
	sha512HexLen   = 128
)

var (
	// ErrInvalidDigest is returned when a manifest route digest is not a valid 128-hex SHA-512 string.
	ErrInvalidDigest = errors.New("casv11: invalid sha512 hex digest")
)

// SiteManifest maps virtual HTTP request paths (e.g. "/index.html") to their SHA-512 CAS digests.
type SiteManifest struct {
	Routes map[string]string `json:"routes"`
}

// ParseSiteManifest decodes a JSON SiteManifest document.
func ParseSiteManifest(data []byte) (*SiteManifest, error) {
	var m SiteManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("casv11: parse site manifest: %w", err)
	}
	if m.Routes == nil {
		m.Routes = make(map[string]string)
	}
	return &m, nil
}

// LoadSiteManifest reads and parses a JSON SiteManifest file from disk.
func LoadSiteManifest(manifestPath string) (*SiteManifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("casv11: read site manifest: %w", err)
	}
	return ParseSiteManifest(data)
}

// CasV11PagesHandler serves zero-extraction static Pages directly from a unified CAS v11 store:
//
//	<casRoot>/files/<sha512[:2]>/<sha512>
type CasV11PagesHandler struct {
	casRoot  string
	filesDir string
	manifest *SiteManifest
}

// NewCasV11PagesHandler creates a handler backed by casRoot and the provided SiteManifest.
func NewCasV11PagesHandler(casRoot string, manifest *SiteManifest) (*CasV11PagesHandler, error) {
	if strings.TrimSpace(casRoot) == "" {
		return nil, errors.New("casv11: casRoot must not be empty")
	}
	if manifest == nil {
		return nil, errors.New("casv11: manifest must not be nil")
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(casRoot))
	if err != nil {
		return nil, fmt.Errorf("casv11: resolve casRoot: %w", err)
	}
	return &CasV11PagesHandler{
		casRoot:  cleanRoot,
		filesDir: filepath.Join(cleanRoot, "files"),
		manifest: manifest,
	}, nil
}

// ServeHTTP handles GET/HEAD requests for static Pages assets mapped by SiteManifest.
func (h *CasV11PagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(casHeaderName, casHeaderValue)

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if hasPathTraversal(r.URL.Path) || hasPathTraversal(r.URL.RawPath) || hasPathTraversal(r.RequestURI) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	routePath := path.Clean("/" + r.URL.Path)
	digestRaw, ok := h.manifest.Routes[routePath]
	if !ok && routePath == "/" {
		routePath = "/index.html"
		digestRaw, ok = h.manifest.Routes[routePath]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	digest, err := normalizeDigest(digestRaw)
	if err != nil {
		http.Error(w, "invalid manifest digest", http.StatusInternalServerError)
		return
	}

	blobPath := filepath.Join(h.filesDir, digest[:2], digest)
	if !isWithinDir(h.filesDir, blobPath) {
		http.Error(w, "invalid blob path", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(blobPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to stat blob", http.StatusInternalServerError)
		return
	}

	etag := `"` + digest + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	data, err := os.ReadFile(blobPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to read blob", http.StatusInternalServerError)
		return
	}

	sum := sha512.Sum512(data)
	if hex.EncodeToString(sum[:]) != digest {
		http.Error(w, "blob integrity verification failed", http.StatusInternalServerError)
		return
	}

	contentType := mime.TypeByExtension(path.Ext(routePath))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func hasPathTraversal(p string) bool {
	if p == "" {
		return false
	}
	if strings.ContainsRune(p, '\x00') || strings.ContainsRune(p, '\\') {
		return true
	}
	lower := strings.ToLower(p)
	if strings.Contains(lower, "%2e%2e") || strings.Contains(lower, "%5c") {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func normalizeDigest(sha512Hex string) (string, error) {
	if len(sha512Hex) != sha512HexLen {
		return "", fmt.Errorf("%w: got length %d, want %d", ErrInvalidDigest, len(sha512Hex), sha512HexLen)
	}
	lower := strings.ToLower(sha512Hex)
	if _, err := hex.DecodeString(lower); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDigest, err)
	}
	return lower, nil
}

func isWithinDir(baseDir, targetPath string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func etagMatches(ifNoneMatch, etag string) bool {
	if strings.TrimSpace(ifNoneMatch) == "" {
		return false
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		c := strings.TrimSpace(candidate)
		if c == "*" || c == etag || c == "W/"+etag {
			return true
		}
	}
	return false
}
