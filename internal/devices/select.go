package devices

import (
	"fmt"
	"strings"
)

// SelectDeviceInfos resolves the same comma-separated labels, groups, locations,
// and IDs accepted by the LIFX controller against already discovered devices.
func SelectDeviceInfos(infos []DeviceInfo, target string) ([]DeviceInfo, error) {
	selectors := splitInfoSelectors(target)
	if len(selectors) == 0 {
		return nil, fmt.Errorf("target is required")
	}

	seen := make(map[string]bool)
	selected := make([]DeviceInfo, 0, len(infos))
	add := func(info DeviceInfo) {
		if info.ID == "" || seen[info.ID] {
			return
		}
		seen[info.ID] = true
		selected = append(selected, info)
	}
	for _, selector := range selectors {
		if selector == "all" {
			for _, info := range infos {
				add(info)
			}
			continue
		}
		for _, info := range infos {
			if matchesInfo(selector, info) {
				add(info)
			}
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no discovered devices match target %q", target)
	}
	return selected, nil
}

func ControllableLights(infos []DeviceInfo) []DeviceInfo {
	selected := make([]DeviceInfo, 0, len(infos))
	for _, info := range infos {
		if info.Capabilities.Kind != DeviceKindSwitch {
			selected = append(selected, info)
		}
	}
	return selected
}

func matchesInfo(selector string, info DeviceInfo) bool {
	return strings.EqualFold(info.ID, selector) ||
		strings.EqualFold(info.Label, selector) ||
		strings.EqualFold(info.Group, selector) ||
		strings.EqualFold(info.Location, selector)
}

func splitInfoSelectors(target string) []string {
	parts := strings.Split(target, ",")
	selectors := make([]string, 0, len(parts))
	for _, part := range parts {
		if selector := strings.ToLower(strings.TrimSpace(part)); selector != "" {
			selectors = append(selectors, selector)
		}
	}
	return selectors
}
