package analyzerbin

import "testing"

// The identity has to follow the bundle's contents, so a changed analyzer always
// replaces an installed copy. The hand-maintained constant this replaced could be
// forgotten, which left a stale analyzer extracted in the user's home.
func TestBundleVersionFollowsContents(t *testing.T) {
	first := bundleVersion([]byte("analyzer one"))
	again := bundleVersion([]byte("analyzer one"))
	other := bundleVersion([]byte("analyzer two"))

	if first != again {
		t.Fatalf("same bundle produced %q then %q", first, again)
	}
	if first == other {
		t.Fatal("different bundles produced the same version")
	}
	if len(first) != 16 {
		t.Fatalf("version %q should be 16 hex characters", first)
	}
}

func TestVersionIsStable(t *testing.T) {
	if Version() == "" {
		t.Fatal("version is empty")
	}
	if Version() != Version() {
		t.Fatal("version changed between calls")
	}
}
