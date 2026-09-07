//go:build !linux && !darwin && !windows

package process

func applyResourceLimits(_ int, _ ResourceLimits) error { return nil }

func platformValidateResourceLimits(limits ResourceLimits) error {
	if limits != (ResourceLimits{}) {
		return ErrUnsupportedResourceLimit
	}
	return nil
}
