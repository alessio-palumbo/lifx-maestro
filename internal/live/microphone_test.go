package live

import (
	"testing"

	"github.com/gen2brain/malgo"
)

func TestSelectCaptureDeviceDefaultsWithoutAssumingFirstDevice(t *testing.T) {
	infos := []malgo.DeviceInfo{{}, {IsDefault: 1}}
	selected, err := selectCaptureDevice(infos, "default")
	if err != nil {
		t.Fatal(err)
	}
	if selected != nil {
		t.Fatal("default selection should leave the device ID nil for miniaudio")
	}
	if got := findDefaultCaptureDevice(infos); got != &infos[1] {
		t.Fatal("default device metadata was not found")
	}
}

func TestSelectCaptureDeviceReportsMissingSelector(t *testing.T) {
	_, err := selectCaptureDevice([]malgo.DeviceInfo{{}}, "studio microphone")
	if err == nil {
		t.Fatal("expected missing microphone error")
	}
}
