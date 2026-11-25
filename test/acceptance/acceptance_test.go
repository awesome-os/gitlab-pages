package acceptance_test

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"testing"

	"go.uber.org/goleak"

	"gitlab.com/gitlab-org/gitlab-pages/internal/fixture"
)

const (
	objectStorageMockServer = "127.0.0.1:" + objectStoragePort
)

var (
	pagesBinary = flag.String("gitlab-pages-binary", "../../gitlab-pages", "Path to the gitlab-pages binary")

	// Ports are assigned per start by assignFreePorts; "0" is a placeholder.
	listeners = []ListenSpec{
		{"http", "127.0.0.1", "0", false},
		{"http", "::1", "0", false},
		{"https", "127.0.0.1", "0", false},
		{"https", "::1", "0", false},
		{"proxy", "127.0.0.1", "0", false},
		{"proxy", "::1", "0", false},
		{"https-proxyv2", "127.0.0.1", "0", false},
		{"https-proxyv2", "::1", "0", false},
		{"https", "127.0.0.1", "0", true},
	}

	ipv4Listeners = []ListenSpec{
		listeners[0],
		listeners[2],
		listeners[4],
		listeners[6],
	}

	httpListener         = listeners[0]
	httpsListener        = listeners[2]
	proxyListener        = listeners[4]
	httpsProxyv2Listener = listeners[6]
	clientCertListener   = listeners[8]
)

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		log.Println("Acceptance tests disabled")
		os.Exit(0)
	}

	if _, err := os.Stat(*pagesBinary); errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("Couldn't find gitlab-pages binary at %s\n", *pagesBinary)
	}

	if ok := TestCertPool.AppendCertsFromPEM([]byte(fixture.Certificate)); !ok {
		fmt.Println("Failed to load cert!")
	}

	goleak.VerifyTestMain(m)
}
