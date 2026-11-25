package disk

import (
	"context"
	"math"
	"mime"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
)

const (
	maxAcceptParts = 16

	wildcardMimeType = "*/*"

	mimeRdfXML        = "application/rdf+xml"
	mimeTurtle        = "text/turtle"
	mimeTurtleUTF8    = "text/turtle; charset=utf-8"
	mimeXTurtle       = "application/x-turtle"
	mimeN3            = "text/n3"
	mimeN3UTF8        = "text/n3; charset=utf-8"
	mimeRdfN3         = "text/rdf+n3"
	mimeNTriples      = "application/n-triples"
	mimeLdJSON        = "application/ld+json"
	mimeOwlXML        = "application/owl+xml"
	mimeTextPlainUTF8 = "text/plain; charset=UTF-8"
	mimeTextHTML      = "text/html"
	mimeXhtmlXML      = "application/xhtml+xml"

	extRDF    = ".rdf"
	extTTL    = ".ttl"
	extN3     = ".n3"
	extNT     = ".nt"
	extJSONLD = ".jsonld"
	extOwl    = ".owl"
	extOWX    = ".owx"
	extTxt    = ".txt"
	extHTML   = ".html"
)

// mimeCandidate represents a parsed media range from the Accept header.
type mimeCandidate struct {
	mediaType   string
	quality     float64
	specificity int
	order       int
}

// knownMimeExtensions maps supported semantic web media types to file extensions.
var knownMimeExtensions = map[string][]string{
	mimeRdfXML:   {extRDF, extOwl},
	mimeTurtle:   {extTTL},
	mimeXTurtle:  {extTTL},
	mimeN3:       {extN3},
	mimeRdfN3:    {extN3},
	mimeNTriples: {extNT},
	mimeLdJSON:   {extJSONLD},
	mimeOwlXML:   {extOWX},
	mimeTextHTML: {extHTML},
	mimeXhtmlXML: {extHTML},
}

func parseQValue(qStr string) (float64, bool) {
	qStr = strings.TrimSpace(qStr)
	q, err := strconv.ParseFloat(qStr, 64)
	if err != nil || math.IsNaN(q) || math.IsInf(q, 0) || q < 0.0 || q > 1.0 {
		return 0, false
	}
	return q, true
}

func parseQuality(params map[string]string, rawPart string) (float64, bool) {
	if params != nil {
		if qStr, ok := params["q"]; ok {
			return parseQValue(qStr)
		}
		return 1.0, true
	}

	// When mime.ParseMediaType fails, extract q= parameter from rawPart
	// to avoid treating malformed entries with q=0 as q=1.0.
	if idx := strings.Index(rawPart, ";"); idx != -1 {
		for _, param := range strings.Split(rawPart[idx+1:], ";") {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(strings.ToLower(param), "q=") {
				return parseQValue(param[2:])
			}
		}
	}

	return 1.0, true
}

func mimeSpecificity(mediaType string) int {
	if mediaType == wildcardMimeType {
		return 0
	}
	if strings.HasSuffix(mediaType, "/*") {
		return 1
	}
	return 2
}

func parseCandidate(part string, order int) (mimeCandidate, bool) {
	mediaType, params, err := mime.ParseMediaType(part)
	if err != nil {
		rawType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if rawType == "" {
			return mimeCandidate{}, false
		}
		mediaType = strings.ToLower(rawType)
	}

	mediaType = strings.ToLower(mediaType)
	quality, ok := parseQuality(params, part)
	if !ok || quality <= 0 {
		return mimeCandidate{}, false
	}

	return mimeCandidate{
		mediaType:   mediaType,
		quality:     quality,
		specificity: mimeSpecificity(mediaType),
		order:       order,
	}, true
}

func sortCandidates(candidates []mimeCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].quality != candidates[j].quality {
			return candidates[i].quality > candidates[j].quality
		}
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity > candidates[j].specificity
		}
		return candidates[i].order < candidates[j].order
	})
}

// parseAcceptHeader parses an HTTP Accept header according to RFC 9110 Section 12.5.1.
// It returns candidates sorted by quality descending, then specificity descending,
// then original order. It bounds parsing work to at most maxAcceptParts.
func parseAcceptHeader(header string) []mimeCandidate {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}

	seenTypes := make(map[string]struct{})
	candidates := make([]mimeCandidate, 0, maxAcceptParts)

	for parsedParts := 0; len(header) > 0 && parsedParts < maxAcceptParts; parsedParts++ {
		var part string
		if idx := strings.IndexByte(header, ','); idx != -1 {
			part = header[:idx]
			header = header[idx+1:]
		} else {
			part = header
			header = ""
		}

		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		candidate, ok := parseCandidate(part, parsedParts)
		if !ok {
			continue
		}

		if _, seen := seenTypes[candidate.mediaType]; seen {
			continue
		}
		seenTypes[candidate.mediaType] = struct{}{}

		candidates = append(candidates, candidate)
	}

	sortCandidates(candidates)
	return candidates
}

func extensionsForMediaType(mediaType string) []string {
	if mediaType == wildcardMimeType {
		return []string{extHTML}
	}
	return knownMimeExtensions[mediaType]
}

func (reader *Reader) tryExtensions(ctx context.Context, root vfs.Root, basePath string, exts []string, seenExts map[string]struct{}) (string, error) {
	var lastErr = os.ErrNotExist
	for _, ext := range exts {
		if _, seen := seenExts[ext]; seen {
			continue
		}
		seenExts[ext] = struct{}{}

		fullPath, err := reader.resolvePath(ctx, root, basePath+ext)
		if err == nil {
			return fullPath, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// negotiatePath resolves a subpath with an extension based on the Accept header.
// It tries to find existing matching files in order of client preference.
// If none are found, it falls back to .html.
// It returns the resolved path, a boolean indicating whether content negotiation was performed,
// and an error if no matching file exists.
func (reader *Reader) negotiatePath(ctx context.Context, root vfs.Root, basePath string, r *http.Request) (string, bool, error) {
	accept := r.Header.Get("Accept")
	candidates := parseAcceptHeader(accept)

	// With no usable Accept header the URL is still negotiable, so keep Vary: Accept
	// so shared caches do not serve this HTML to clients that ask for another type.
	if len(candidates) == 0 {
		fullPath, err := reader.resolvePath(ctx, root, basePath+extHTML)
		return fullPath, true, err
	}

	seenExts := make(map[string]struct{}, len(candidates)+1)

	for _, candidate := range candidates {
		exts := extensionsForMediaType(candidate.mediaType)
		if fullPath, err := reader.tryExtensions(ctx, root, basePath, exts, seenExts); err == nil {
			return fullPath, true, nil
		}
	}

	// If no preferred format exists on disk, fall back to .html
	fullPath, err := reader.tryExtensions(ctx, root, basePath, []string{extHTML}, seenExts)
	if err == nil {
		return fullPath, true, nil
	}
	return "", false, err
}
