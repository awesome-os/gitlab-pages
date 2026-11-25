package main

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"gitlab.com/gitlab-org/labkit/errortracking"
	"gitlab.com/gitlab-org/labkit/fips"
	"go.uber.org/automaxprocs/maxprocs"

	cfg "gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/validateargs"
	"gitlab.com/gitlab-org/gitlab-pages/metrics"
)

func init() {
	_, err := maxprocs.Set()
	if err != nil {
		logging.Logger.Error("could not set automaxprocs", "error", err)
	}
}

// VERSION stores the information about the semantic version of application
var VERSION = "dev"

// REVISION stores the information about the git revision of application
var REVISION = "HEAD"

func initErrorReporting(sentryDSN, sentryEnvironment string) error {
	return errortracking.Initialize(
		errortracking.WithSentryDSN(sentryDSN),
		errortracking.WithVersion(fmt.Sprintf("%s-%s", VERSION, REVISION)),
		errortracking.WithLoggerName("gitlab-pages"),
		errortracking.WithSentryEnvironment(sentryEnvironment))
}

func appMain() error {
	if err := validateargs.NotAllowed(os.Args[1:]); err != nil {
		return fmt.Errorf("using invalid arguments, use -config=gitlab-pages-config file instead: %w", err)
	}

	if err := validateargs.Deprecated(os.Args[1:]); err != nil {
		logging.Logger.Warn("Using deprecated arguments", "error", err)
	}

	config, err := cfg.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	printVersion(config.General.ShowVersion, VERSION)

	if err := cfg.Validate(config); err != nil {
		return fmt.Errorf("invalid config settings: %w", err)
	}

	if config.Sentry.DSN != "" {
		err := initErrorReporting(config.Sentry.DSN, config.Sentry.Environment)
		if err != nil {
			logging.Logger.Warn("Failed to initialize errortracking", "error", err)
		}
	}

	err = logging.ConfigureLogging(config.Log.Format, config.Log.Verbose, config.Log.FieldSchemaVersion, config.Log.FieldDualEmitTarget)
	if err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}

	cfg.LogConfig(config)

	logging.Logger.Info("GitLab Pages", "version", VERSION, "revision", REVISION)
	logging.Logger.Info("URL: https://gitlab.com/gitlab-org/gitlab-pages")

	if config.GitLab.EnableDisk {
		if err := os.Chdir(config.General.RootDir); err != nil {
			return fmt.Errorf("could not change directory into pagesRoot: %w", err)
		}
	}
	fips.Check()

	return runApp(config)
}

func printVersion(showVersion bool, version string) {
	if showVersion {
		fmt.Fprintf(os.Stdout, "%s\n", version)
		os.Exit(0)
	}
}

func main() {
	logrus.SetOutput(os.Stderr)

	metrics.MustRegister()

	if err := appMain(); err != nil {
		logging.Logger.Error(err.Error(), "error", err)
		os.Exit(1)
	}
}
