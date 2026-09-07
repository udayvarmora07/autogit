//go:build windows

package process

func applyResourceLimits(_ int, _ ResourceLimits) error { return nil }

func platformValidateResourceLimits(limits ResourceLimits) error {
	if limits.FileBytes != 0 {
		return ErrUnsupportedResourceLimit
	}
	return nil
}
