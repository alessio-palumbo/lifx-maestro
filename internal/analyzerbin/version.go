package analyzerbin

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

var (
	versionOnce  sync.Once
	versionValue string
)

// Version identifies the embedded analyzer bundle by its contents.
//
// It replaced a hand-maintained constant. That constant had to be bumped whenever
// analyze.py or its dependencies changed, because the extracted copy lives in the
// user's home directory and outlives any reinstall of the app. Forgetting to bump
// it left a stale analyzer installed: invisible in development, where nothing is
// bundled, and wrong only in released builds. Deriving the identity from the bundle
// makes it automatic and exactly right — different bytes, fresh install.
func Version() string {
	versionOnce.Do(func() {
		versionValue = bundleVersion(analyzerZip)
	})
	return versionValue
}

func bundleVersion(bundle []byte) string {
	sum := sha256.Sum256(bundle)
	// Half the digest is far more than enough to tell two bundles apart, and keeps
	// the marker files readable.
	return hex.EncodeToString(sum[:8])
}
