// Package config handles loading and parsing YAML configuration files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} or ${VAR_NAME:-default} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// LoadConfig loads a single YAML configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Substitute environment variables
	content := substituteEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config in %s: %w", path, err)
	}

	return &cfg, nil
}

// LoadConfigDir loads all YAML files from a directory.
func LoadConfigDir(dir string) ([]*Config, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read config directory %s: %w", dir, err)
	}

	var configs []*Config
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		cfg, err := LoadConfig(path)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no YAML configuration files found in %s", dir)
	}

	return configs, nil
}

// substituteEnvVars replaces ${VAR} and ${VAR:-default} patterns with environment values.
func substituteEnvVars(content string) string {
	return envVarPattern.ReplaceAllStringFunc(content, func(match string) string {
		submatches := envVarPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		varName := submatches[1]
		defaultVal := ""
		if len(submatches) >= 3 {
			defaultVal = submatches[2]
		}

		if val, exists := os.LookupEnv(varName); exists {
			return val
		}
		return defaultVal
	})
}

// validateConfig performs basic validation on the configuration.
func validateConfig(cfg *Config) error {
	if cfg.Version == "" {
		cfg.Version = "1.0"
	}

	if cfg.Exporter.Endpoint == "" {
		return fmt.Errorf("exporter.endpoint is required")
	}

	if cfg.Exporter.Protocol == "" {
		cfg.Exporter.Protocol = "http/protobuf"
	}

	if cfg.Simulation.TickInterval == "" {
		cfg.Simulation.TickInterval = "100ms"
	}

	if cfg.Simulation.Duration == "" {
		cfg.Simulation.Duration = "infinite"
	}

	if len(cfg.Services) == 0 {
		return fmt.Errorf("at least one service must be defined")
	}

	// Validate each service
	serviceNames := make(map[string]bool)
	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return fmt.Errorf("service[%d].name is required", i)
		}
		if serviceNames[svc.Name] {
			return fmt.Errorf("duplicate service name: %s", svc.Name)
		}
		serviceNames[svc.Name] = true

		// Set default type based on system
		if svc.Type == "" {
			switch svc.System {
			case "postgresql", "mysql", "redis", "mongodb":
				cfg.Services[i].Type = "database"
			case "kafka", "rabbitmq":
				cfg.Services[i].Type = "messaging"
			default:
				cfg.Services[i].Type = "http"
			}
		}

		// Validate endpoints
		for j, ep := range svc.Endpoints {
			if ep.Name == "" {
				return fmt.Errorf("service[%s].endpoint[%d].name is required", svc.Name, j)
			}

			// Validate downstream calls reference existing services
			for k, call := range ep.Calls {
				if call.Service == "" {
					return fmt.Errorf("service[%s].endpoint[%s].calls[%d].service is required", svc.Name, ep.Name, k)
				}
				if call.Endpoint == "" {
					return fmt.Errorf("service[%s].endpoint[%s].calls[%d].endpoint is required", svc.Name, ep.Name, k)
				}
			}
		}
	}

	return nil
}

// FindServiceByName finds a service configuration by name.
func (c *Config) FindServiceByName(name string) *ServiceConfig {
	for i := range c.Services {
		if c.Services[i].Name == name {
			return &c.Services[i]
		}
	}
	return nil
}

// FindEndpointByName finds an endpoint within a service.
func (s *ServiceConfig) FindEndpointByName(name string) *EndpointConfig {
	for i := range s.Endpoints {
		if s.Endpoints[i].Name == name {
			return &s.Endpoints[i]
		}
	}
	return nil
}

// GetEntryPoints returns all endpoints that generate traffic (have traffic config).
func (c *Config) GetEntryPoints() []struct {
	Service  *ServiceConfig
	Endpoint *EndpointConfig
} {
	var entryPoints []struct {
		Service  *ServiceConfig
		Endpoint *EndpointConfig
	}

	for i := range c.Services {
		svc := &c.Services[i]
		for j := range svc.Endpoints {
			ep := &svc.Endpoints[j]
			if ep.Traffic != nil && ep.Traffic.RequestsPerMinute > 0 {
				entryPoints = append(entryPoints, struct {
					Service  *ServiceConfig
					Endpoint *EndpointConfig
				}{svc, ep})
			}
		}
	}

	return entryPoints
}

// GetResourceAttributes returns merged resource attributes for a service.
func (c *Config) GetResourceAttributes(svc *ServiceConfig) map[string]string {
	attrs := make(map[string]string)

	// Copy global attributes
	for k, v := range c.GlobalResource {
		attrs[k] = v
	}

	// Override/add service-specific attributes
	for k, v := range svc.ResourceAttributes {
		attrs[k] = v
	}

	// Add service name if not present
	if _, ok := attrs["service.name"]; !ok {
		attrs["service.name"] = svc.Name
	}

	// Add service version if available
	if svc.Version != "" {
		if _, ok := attrs["service.version"]; !ok {
			attrs["service.version"] = svc.Version
		}
	}

	return attrs
}
