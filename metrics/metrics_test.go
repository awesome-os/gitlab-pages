package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// The "status_code" label is an external contract: dashboards and alerts query
// the GitLab API metrics by it. This pins the label name so a refactor of the
// shared statusCodeLabel constant cannot silently rename it.
func TestAPIRequestMetricExposesStatusCodeLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(DomainsSourceAPIReqTotal))
	t.Cleanup(DomainsSourceAPIReqTotal.Reset)

	DomainsSourceAPIReqTotal.WithLabelValues("200").Inc()

	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)

	var labelNames []string
	for _, metric := range families[0].GetMetric() {
		for _, label := range metric.GetLabel() {
			labelNames = append(labelNames, label.GetName())
		}
	}

	require.Contains(t, labelNames, "status_code")
}
