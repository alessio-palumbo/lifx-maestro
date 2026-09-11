package devices

import "testing"

func TestSelectDeviceInfosResolvesAndDeduplicatesSelectors(t *testing.T) {
	infos := []DeviceInfo{
		{ID: "one", Label: "Desk", Group: "office", Location: "home"},
		{ID: "two", Label: "Strip", Group: "tv", Location: "home"},
	}
	selected, err := SelectDeviceInfos(infos, "home,desk")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].ID != "one" || selected[1].ID != "two" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestControllableLightsExcludesOnlySwitches(t *testing.T) {
	infos := []DeviceInfo{
		{ID: "color", Capabilities: DeviceCapabilities{Kind: DeviceKindSingleZone, HasColor: true}},
		{ID: "white", Capabilities: DeviceCapabilities{Kind: DeviceKindSingleZone}},
		{ID: "switch", Capabilities: DeviceCapabilities{Kind: DeviceKindSwitch, HasColor: true}},
	}
	selected := ControllableLights(infos)
	if len(selected) != 2 || selected[0].ID != "color" || selected[1].ID != "white" {
		t.Fatalf("selected = %#v", selected)
	}
}
