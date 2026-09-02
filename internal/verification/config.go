package verification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
)

const (
	configVersion      = "1"
	defaultConfigLimit = int64(1 << 20)
	maxVerifierTimeout = 10 * time.Minute
	maxVerifierOutput  = 16 << 20
)

type verifierConfig struct {
	Version   string                `json:"version"`
	Verifiers []verifierConfigEntry `json:"verifiers"`
}

type verifierConfigEntry struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Argv        []string          `json:"argv"`
	Applicable  *bool             `json:"applicable"`
	TimeoutMS   *int64            `json:"timeout_ms"`
	MaxOutput   *int              `json:"max_output"`
	Environment map[string]string `json:"environment"`
}

// LoadRegistry parses a trusted verifier configuration and returns the same
// frozen registry used by the verification boundary. Unknown JSON fields,
// trailing values, oversized input, and unsafe resource limits are rejected.
func LoadRegistry(raw []byte, max int64) (*VerifierRegistry, error) {
	if max <= 0 {
		max = defaultConfigLimit
	}
	if int64(len(raw)) > max {
		return nil, errors.New("verifier configuration exceeds input limit")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var config verifierConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode verifier configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("verifier configuration has trailing data")
		}
		return nil, fmt.Errorf("decode verifier configuration trailer: %w", err)
	}
	if config.Version != configVersion {
		return nil, fmt.Errorf("unsupported verifier configuration version %q", config.Version)
	}
	specs := make([]TrustedVerifierSpec, 0, len(config.Verifiers))
	for _, entry := range config.Verifiers {
		applicable := true
		if entry.Applicable != nil {
			applicable = *entry.Applicable
		}
		timeout := time.Duration(0)
		if entry.TimeoutMS != nil {
			if *entry.TimeoutMS <= 0 {
				return nil, fmt.Errorf("verifier %q has invalid timeout", entry.Name)
			}
			if *entry.TimeoutMS > int64(maxVerifierTimeout/time.Millisecond) {
				return nil, fmt.Errorf("verifier %q timeout exceeds limit", entry.Name)
			}
			timeout = time.Duration(*entry.TimeoutMS) * time.Millisecond
		}
		maxOutput := 0
		if entry.MaxOutput != nil {
			if *entry.MaxOutput <= 0 || *entry.MaxOutput > maxVerifierOutput {
				return nil, fmt.Errorf("verifier %q has invalid output limit", entry.Name)
			}
			maxOutput = *entry.MaxOutput
		}
		specs = append(specs, TrustedVerifierSpec{
			Name: entry.Name, Version: entry.Version, Argv: append([]string(nil), entry.Argv...),
			Applicable: applicable, Timeout: timeout, MaxOutput: maxOutput,
			Environment: cloneEnvironment(entry.Environment),
		})
	}
	return NewVerifierRegistry(specs)
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := consumeJSONValue(decoder); err != nil {
		return fmt.Errorf("decode verifier configuration: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("verifier configuration has trailing data")
		}
		return fmt.Errorf("decode verifier configuration trailer: %w", err)
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate configuration field %q", key)
			}
			seen[key] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

// LoadRegistryFile reads a trusted configuration with an explicit byte cap.
func LoadRegistryFile(path string, max int64) (*VerifierRegistry, error) {
	if path == "" {
		return nil, errors.New("verifier configuration path is required")
	}
	if max <= 0 {
		max = defaultConfigLimit
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("verifier configuration must be a regular non-symlink file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("verifier configuration permissions are too broad")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	return LoadRegistry(raw, max)
}
