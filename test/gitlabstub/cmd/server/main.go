package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gitlab.com/gitlab-org/gitlab-pages/test/gitlabstub"
)

var (
	pagesRoot  = flag.String("pages-root", "shared/pages", "The directory where pages are stored")
	keyFile    = flag.String("key-file", "", "Path to file certificate")
	certFile   = flag.String("cert-file", "", "Path to file certificate")
	caCertPath = flag.String("ca-crt", "", "CA certificate for mutual TLS")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	server, err := createServer()
	if err != nil {
		return fmt.Errorf("error starting the server: %w", err)
	}

	if server.TLS != nil {
		server.StartTLS()
	} else {
		server.Start()
	}

	log.Printf("listening on %s\n", server.URL)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	<-sigChan

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Config.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error shutting down %w", err)
	}

	return nil
}

func createServer() (*httptest.Server, error) {
	var opts []gitlabstub.Option

	if cert, err := loadCertificate(*certFile, *keyFile); err != nil {
		return nil, err
	} else if cert != nil {
		opts = append(opts, gitlabstub.WithCertificate(*cert))
	}

	if wd, err := filepath.Abs(*pagesRoot); err != nil {
		return nil, err
	} else if *pagesRoot != "" {
		opts = append(opts, gitlabstub.WithPagesRoot(wd))
	}

	if caCert, err := loadCACertificate(*caCertPath); err != nil {
		return nil, err
	} else if caCert != nil {
		opts = append(opts, gitlabstub.WithMutualTLS(caCert))
	}

	server, err := gitlabstub.NewUnstartedServer(opts...)
	if err != nil {
		return nil, fmt.Errorf("error starting the server: %w", err)
	}

	return server, err
}

func loadCertificate(cert string, key string) (*tls.Certificate, error) {
	if cert == "" && key == "" {
		return nil, nil
	}

	if cert != "" && key != "" {
		cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading certificate: %w", err)
		}

		return &cert, nil
	}

	return nil, fmt.Errorf("missing certificate or key: cert(%s) key(%s)", cert, key)
}
func loadCACertificate(certPath string) (*x509.Certificate, error) {
	if certPath == "" {
		return nil, nil
	}

	caCertFile, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("error reading CA file: %w", err)
	}

	block, _ := pem.Decode(caCertFile)

	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("error parsing CA certificate: %w", err)
	}

	return caCert, nil
}
