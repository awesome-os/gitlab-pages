package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gitlab.com/gitlab-org/labkit/correlation"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/api"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/client"
)

// Retriever is an utility type that performs an HTTP request with backoff in
// case of errors
type Retriever struct {
	client               api.Client
	retrievalTimeout     time.Duration
	maxRetrievalInterval time.Duration
	maxRetrievalRetries  int
}

// NewRetriever creates a Retriever with a client
func NewRetriever(client api.Client, retrievalTimeout, maxRetrievalInterval time.Duration, maxRetrievalRetries int) *Retriever {
	return &Retriever{
		client:               client,
		retrievalTimeout:     retrievalTimeout,
		maxRetrievalInterval: maxRetrievalInterval,
		maxRetrievalRetries:  maxRetrievalRetries,
	}
}

// Retrieve retrieves a lookup response from external source with timeout and
// backoff. It has its own context with timeout.
func (r *Retriever) Retrieve(correlationID, domain string) (lookup api.Lookup) {
	var logMsg string

	ctx := correlation.ContextWithCorrelation(context.Background(), correlationID)
	ctx = logv2.WithLogger(ctx, logging.Logger)

	ctx, cancel := context.WithTimeout(ctx, r.retrievalTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		logMsg = "retrieval context done"
		lookup = api.Lookup{Name: domain, Error: fmt.Errorf(logMsg+": %w", ctx.Err())}
	case lookup = <-r.resolveWithBackoff(ctx, domain):
		logMsg = "retrieval response sent"
	}

	logv2.FromContext(ctx).DebugContext(ctx, logMsg,
		logv2.CorrelationID(correlationID),
		slog.String("requested_domain", domain),
		slog.String("lookup_name", lookup.Name),
		slog.Int("lookup_paths_count", len(lookup.LookupPaths())),
		slog.Any("lookup_paths", api.RedactedLookupPaths(lookup.LookupPaths())),
		slog.Any("lookup_error", logging.CleanURLError(lookup.Error)),
		logging.NillableError(ctx.Err()),
	)

	return lookup
}

func (r *Retriever) resolveWithBackoff(ctx context.Context, domainName string) <-chan api.Lookup {
	response := make(chan api.Lookup)

	go func() {
		var lookup api.Lookup

		for i := 1; i <= r.maxRetrievalRetries; i++ {
			lookup = r.client.GetLookup(ctx, domainName)
			if lookup.Error == nil || errors.Is(lookup.Error, domain.ErrDomainDoesNotExist) ||
				errors.Is(lookup.Error, client.ErrUnauthorizedAPI) {
				// do not retry if the domain does not exist or there is an auth error
				break
			}

			if errors.Is(lookup.Error, context.Canceled) || errors.Is(lookup.Error, context.DeadlineExceeded) {
				// do not retry if there's a context error to avoid leaking the goroutine
				break
			}

			time.Sleep(r.maxRetrievalInterval)
		}

		// Use select to avoid blocking if the context is done
		select {
		case response <- lookup:
			close(response)
		case <-ctx.Done():
		}
	}()

	return response
}
