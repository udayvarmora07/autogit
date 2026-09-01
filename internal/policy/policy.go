package policy

import "fmt"

type Policy struct {
	Tracking         string `json:"tracking,omitempty"`
	Visibility       string `json:"visibility,omitempty"`
	Provider         string `json:"provider,omitempty"`
	Owner            string `json:"owner,omitempty"`
	Destination      string `json:"destination,omitempty"`
	Workflow         string `json:"workflow,omitempty"`
	LocalOnly        bool   `json:"local_only,omitempty"`
	PublicConsent    bool   `json:"public_consent,omitempty"`
	LocalOnlySet     bool   `json:"-"`
	PublicConsentSet bool   `json:"-"`
	Version          int    `json:"version,omitempty"`
}

func Merge(base, project Policy) Policy {
	if project.Tracking != "" {
		base.Tracking = project.Tracking
	}
	if project.Visibility != "" {
		base.Visibility = project.Visibility
	}
	if project.Provider != "" {
		base.Provider = project.Provider
	}
	if project.Owner != "" {
		base.Owner = project.Owner
	}
	if project.Destination != "" {
		base.Destination = project.Destination
	}
	if project.Workflow != "" {
		base.Workflow = project.Workflow
	}
	if project.LocalOnly || project.LocalOnlySet {
		base.LocalOnly = project.LocalOnly
	}
	if project.PublicConsent || project.PublicConsentSet {
		base.PublicConsent = project.PublicConsent
	}
	if project.Version != 0 {
		base.Version = project.Version
	}
	return base
}
func (p Policy) ProviderAllowed() bool { return p.Tracking != "no" && !p.LocalOnly && p.Provider != "" }
func (p Policy) CanPublishPublic() bool {
	return p.Tracking != "no" && p.Visibility == "public" && p.PublicConsent && !p.LocalOnly
}
func (p Policy) TrackingEnabled() bool {
	return p.Tracking == "yes" || p.Tracking == "local" || p.Tracking == "public"
}

func Validate(p Policy) error {
	switch p.Tracking {
	case "", "yes", "no", "local":
	default:
		return fmt.Errorf("invalid tracking policy")
	}
	switch p.Visibility {
	case "", "private", "public":
	default:
		return fmt.Errorf("invalid visibility policy")
	}
	switch p.Workflow {
	case "", "safe", "solo", "checkpoint", "fast":
	default:
		return fmt.Errorf("invalid workflow policy")
	}
	if p.Visibility == "public" && p.PublicConsent && p.LocalOnly {
		return fmt.Errorf("public and local-only policies conflict")
	}
	return nil
}
