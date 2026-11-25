package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/api"
)

type fixedLookupClient struct {
	lookup api.Lookup
}

func (c fixedLookupClient) GetLookup(context.Context, string) api.Lookup {
	return c.lookup
}

func TestRetrieverDoesNotLogLookupSourceURLs(t *testing.T) {
	client := fixedLookupClient{lookup: api.Lookup{
		Name: "example.com",
		Domain: &api.VirtualDomain{LookupPaths: []api.LookupPath{{
			ProjectID: 1,
			Prefix:    "/project/",
			Source: api.Source{
				Type: "zip",
				Path: "https://object-storage.example/archive.zip?X-Amz-Credential=credential-secret&X-Amz-Signature=signature-secret",
			},
		}}},
	}}
	retriever := NewRetriever(client, time.Second, 0, 1)

	buf := loggingtest.Capture(t, slog.LevelDebug)

	lookup := retriever.Retrieve("correlation-id", "example.com")

	require.Equal(t, "example.com", lookup.Name)
	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Equal(t, float64(1), record["lookup_paths_count"])
	require.Equal(t, []any{map[string]any{
		"project_id": float64(1),
		"prefix":     "/project/",
		"source": map[string]any{
			"type": "zip",
			"path": "https://object-storage.example/archive.zip",
		},
	}}, record["lookup_paths"])
	require.NotContains(t, buf.String(), "credential-secret")
	require.NotContains(t, buf.String(), "signature-secret")
}
