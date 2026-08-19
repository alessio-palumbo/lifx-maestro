package devices

import (
	"testing"
	"time"
)

// Restarting a preview quickly used to recapture state while the lights were
// still mid-restore, storing this app's own colours as the user's. These cover the
// decision that prevents it.
func TestHoldsUsableSnapshotWithNothingCaptured(t *testing.T) {
	controller := &LifxDeviceController{}

	if controller.holdsUsableSnapshot("all") {
		t.Fatal("an empty controller should capture rather than reuse")
	}
}

func TestHoldsUsableSnapshotBeforeAnyRestore(t *testing.T) {
	controller := &LifxDeviceController{
		snapshots:      []stateSnapshot{{}},
		snapshotTarget: "all",
	}

	// Never restored, so the stored state is still what the lights looked like
	// before this app touched them.
	if !controller.holdsUsableSnapshot("all") {
		t.Fatal("an unrestored snapshot should be reused")
	}
}

func TestHoldsUsableSnapshotWithinSettleWindow(t *testing.T) {
	controller := &LifxDeviceController{
		snapshots:      []stateSnapshot{{}},
		snapshotTarget: "all",
		restoredAt:     time.Now(),
	}

	if !controller.holdsUsableSnapshot("all") {
		t.Fatal("a snapshot restored just now should be reused, not recaptured mid-fade")
	}
}

func TestHoldsUsableSnapshotAfterSettleWindow(t *testing.T) {
	controller := &LifxDeviceController{
		snapshots:      []stateSnapshot{{}},
		snapshotTarget: "all",
		restoredAt:     time.Now().Add(-stateSettleWindow - time.Second),
	}

	// The lights have settled and the user may have changed them since, so a fresh
	// capture is now the more accurate choice.
	if controller.holdsUsableSnapshot("all") {
		t.Fatal("a settled snapshot should be recaptured")
	}
}

func TestHoldsUsableSnapshotIgnoresADifferentTarget(t *testing.T) {
	controller := &LifxDeviceController{
		snapshots:      []stateSnapshot{{}},
		snapshotTarget: "desk",
		restoredAt:     time.Now(),
	}

	if controller.holdsUsableSnapshot("lounge") {
		t.Fatal("a snapshot for another target should not be reused")
	}
}

// RestoreState stamps the settle window before sending, so the window covers the
// fade rather than starting after it.
func TestRestoreStateStampsTheSettleWindow(t *testing.T) {
	controller := &LifxDeviceController{
		snapshots:      []stateSnapshot{{}},
		snapshotTarget: "all",
	}

	if err := controller.RestoreState(); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
	if controller.restoredAt.IsZero() {
		t.Fatal("restore did not stamp a time, so the settle window never applies")
	}
	if !controller.holdsUsableSnapshot("all") {
		t.Fatal("the snapshot should still be held immediately after restoring")
	}
}

func TestRestoreStateWithoutSnapshotsIsANoOp(t *testing.T) {
	controller := &LifxDeviceController{}

	if err := controller.RestoreState(); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
}
