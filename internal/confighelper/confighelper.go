package confighelper

import (
	"time"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
)

var DefaultZipCfgForTest = config.ZipServing{
	ExpirationInterval: 10 * time.Second,
	CleanupInterval:    5 * time.Second,
	RefreshInterval:    5 * time.Second,
	OpenTimeout:        5 * time.Second,
}

var DefaultCacheConfigForTest = config.Cache{
	CacheExpiry:          time.Second,
	CacheCleanupInterval: time.Second / 2,
	EntryRefreshTimeout:  time.Second / 2,
	RetrievalTimeout:     time.Second,
	MaxRetrievalInterval: time.Second / 3,
	MaxRetrievalRetries:  3,
}
