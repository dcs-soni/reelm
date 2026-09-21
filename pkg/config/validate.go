package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Validate verifies that all configuration values are semantically correct.
func (c *Config) Validate() error {
	var errs []string

	// Validate mode
	switch strings.ToLower(c.Mode) {
	case ModeRecord, ModeReplay, ModeAuto:
	case "":
		errs = append(errs, "mode cannot be empty")
	default:
		errs = append(errs, fmt.Sprintf("invalid mode %q: must be 'record', 'replay', or 'auto'", c.Mode))
	}

	// Validate cassette format
	switch strings.ToLower(c.CassetteFormat) {
	case FormatYAML, FormatJSON:
	case "":
		c.CassetteFormat = FormatYAML
	default:
		errs = append(errs, fmt.Sprintf("invalid cassette_format %q: must be 'yaml' or 'json'", c.CassetteFormat))
	}

	// Validate network endpoints
	if strings.TrimSpace(c.ListenAddr) == "" {
		errs = append(errs, "listen_addr cannot be empty")
	}
	if strings.TrimSpace(c.CassetteDir) == "" {
		errs = append(errs, "cassette_dir cannot be empty")
	}

	// Validate providers
	if len(c.Providers) == 0 {
		errs = append(errs, "at least one provider must be defined")
	}
	for i, p := range c.Providers {
		if strings.TrimSpace(p.Name) == "" {
			errs = append(errs, fmt.Sprintf("providers[%d].name cannot be empty", i))
		}
		if strings.TrimSpace(p.BaseURL) == "" {
			errs = append(errs, fmt.Sprintf("providers[%s].base_url cannot be empty", p.Name))
		} else {
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Scheme == "" || u.Host == "" {
				errs = append(errs, fmt.Sprintf("providers[%s].base_url %q is not a valid absolute URL", p.Name, p.BaseURL))
			}
		}
	}

	// Validate matching threshold
	if c.Matching.SimilarityThreshold < 0.0 || c.Matching.SimilarityThreshold > 1.0 {
		errs = append(errs, fmt.Sprintf("matching.similarity_threshold must be between 0.0 and 1.0, got %f", c.Matching.SimilarityThreshold))
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
