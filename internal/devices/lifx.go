package devices

import "fmt"

type LifxDeviceController struct{}

func NewLifxDeviceController() *LifxDeviceController {
	return &LifxDeviceController{}
}

func (l *LifxDeviceController) PowerOn(target string) error {
	return fmt.Errorf("lifx device controller is not implemented yet")
}

func (l *LifxDeviceController) PowerOff(target string) error {
	return fmt.Errorf("lifx device controller is not implemented yet")
}

func (l *LifxDeviceController) SetColor(target string, params ColorParams) error {
	return fmt.Errorf("lifx device controller is not implemented yet")
}
