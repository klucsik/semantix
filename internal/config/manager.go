// Package config provides configuration management for Semantix.
package config

import (
	"encoding/json"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ChangeType represents the type of configuration change.
type ChangeType string

const (
	ChangeTypeServices        ChangeType = "services"
	ChangeTypeProblemPatterns ChangeType = "problem_patterns"
	ChangeTypeScenarios       ChangeType = "scenarios"
)

// PendingChange represents a single pending configuration change.
type PendingChange struct {
	Type      ChangeType `json:"type"`
	Section   string     `json:"section"`   // e.g., "services", "problem_patterns", "scenarios"
	YAML      string     `json:"yaml"`      // The new YAML content
	Timestamp time.Time  `json:"timestamp"` // When the change was made
}

// ChangelogEntry represents a saved changeset.
type ChangelogEntry struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Changes   []PendingChange `json:"changes"`
	Summary   string          `json:"summary"` // Human-readable summary
}

// Manager manages configuration state, pending changes, and changelog.
type Manager struct {
	mu             sync.RWMutex
	originalConfig *Config          // The original config loaded from file
	currentConfig  *Config          // The currently active config
	pendingChanges []PendingChange  // Changes not yet applied
	changelog      []ChangelogEntry // History of applied changesets
	listeners      []func(*Config)  // Callbacks when config changes
}

// NewManager creates a new configuration manager.
func NewManager(cfg *Config) *Manager {
	// Deep copy the config for original
	originalCopy := deepCopyConfig(cfg)
	currentCopy := deepCopyConfig(cfg)

	return &Manager{
		originalConfig: originalCopy,
		currentConfig:  currentCopy,
		pendingChanges: make([]PendingChange, 0),
		changelog:      make([]ChangelogEntry, 0),
		listeners:      make([]func(*Config), 0),
	}
}

// GetCurrentConfig returns the currently active configuration.
func (m *Manager) GetCurrentConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentConfig
}

// GetOriginalConfig returns the original configuration loaded from file.
func (m *Manager) GetOriginalConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.originalConfig
}

// GetPendingChanges returns all pending changes.
func (m *Manager) GetPendingChanges() []PendingChange {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]PendingChange, len(m.pendingChanges))
	copy(result, m.pendingChanges)
	return result
}

// GetChangelog returns the changelog history.
func (m *Manager) GetChangelog() []ChangelogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ChangelogEntry, len(m.changelog))
	copy(result, m.changelog)
	return result
}

// HasPendingChanges returns true if there are unsaved changes.
func (m *Manager) HasPendingChanges() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pendingChanges) > 0
}

// AddPendingChange adds a new pending change.
func (m *Manager) AddPendingChange(changeType ChangeType, yamlContent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate the YAML by parsing it
	switch changeType {
	case ChangeTypeServices:
		var services []ServiceConfig
		if err := yaml.Unmarshal([]byte(yamlContent), &services); err != nil {
			return err
		}
	case ChangeTypeProblemPatterns:
		var patterns []ProblemPatternConfig
		if err := yaml.Unmarshal([]byte(yamlContent), &patterns); err != nil {
			return err
		}
	case ChangeTypeScenarios:
		var scenarios []ScenarioConfig
		if err := yaml.Unmarshal([]byte(yamlContent), &scenarios); err != nil {
			return err
		}
	}

	// Remove any existing pending change of the same type
	filtered := make([]PendingChange, 0, len(m.pendingChanges))
	for _, pc := range m.pendingChanges {
		if pc.Type != changeType {
			filtered = append(filtered, pc)
		}
	}

	// Add the new change
	filtered = append(filtered, PendingChange{
		Type:      changeType,
		Section:   string(changeType),
		YAML:      yamlContent,
		Timestamp: time.Now(),
	})

	m.pendingChanges = filtered
	return nil
}

// ClearPendingChanges removes all pending changes.
func (m *Manager) ClearPendingChanges() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingChanges = make([]PendingChange, 0)
}

// ApplyPendingChanges applies all pending changes to create a new active config.
// Returns the new config and a changelog entry.
func (m *Manager) ApplyPendingChanges() (*Config, *ChangelogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.pendingChanges) == 0 {
		return m.currentConfig, nil, nil
	}

	// Start with a copy of current config
	newConfig := deepCopyConfig(m.currentConfig)

	// Apply each pending change
	for _, change := range m.pendingChanges {
		switch change.Type {
		case ChangeTypeServices:
			var services []ServiceConfig
			if err := yaml.Unmarshal([]byte(change.YAML), &services); err != nil {
				return nil, nil, err
			}
			newConfig.Services = services

		case ChangeTypeProblemPatterns:
			var patterns []ProblemPatternConfig
			if err := yaml.Unmarshal([]byte(change.YAML), &patterns); err != nil {
				return nil, nil, err
			}
			newConfig.ProblemPatterns = patterns

		case ChangeTypeScenarios:
			var scenarios []ScenarioConfig
			if err := yaml.Unmarshal([]byte(change.YAML), &scenarios); err != nil {
				return nil, nil, err
			}
			newConfig.Scenarios = scenarios
		}
	}

	// Create changelog entry
	entry := ChangelogEntry{
		ID:        time.Now().Format("20060102-150405"),
		Timestamp: time.Now(),
		Changes:   make([]PendingChange, len(m.pendingChanges)),
		Summary:   m.generateSummary(),
	}
	copy(entry.Changes, m.pendingChanges)

	// Update state
	m.currentConfig = newConfig
	m.changelog = append(m.changelog, entry)
	m.pendingChanges = make([]PendingChange, 0)

	// Notify listeners
	for _, listener := range m.listeners {
		go listener(newConfig)
	}

	return newConfig, &entry, nil
}

// ResetToDefaults resets the config back to the original loaded config.
func (m *Manager) ResetToDefaults() *Config {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset to original
	m.currentConfig = deepCopyConfig(m.originalConfig)
	m.pendingChanges = make([]PendingChange, 0)

	// Add to changelog
	entry := ChangelogEntry{
		ID:        time.Now().Format("20060102-150405"),
		Timestamp: time.Now(),
		Changes:   []PendingChange{},
		Summary:   "Reset to defaults",
	}
	m.changelog = append(m.changelog, entry)

	// Notify listeners
	for _, listener := range m.listeners {
		go listener(m.currentConfig)
	}

	return m.currentConfig
}

// ClearChangelog clears the changelog history.
func (m *Manager) ClearChangelog() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changelog = make([]ChangelogEntry, 0)
}

// OnConfigChange registers a callback for config changes.
func (m *Manager) OnConfigChange(listener func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
}

// GetServicesYAML returns the current services config as YAML.
func (m *Manager) GetServicesYAML() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, err := yaml.Marshal(m.currentConfig.Services)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetProblemPatternsYAML returns the current problem patterns config as YAML.
func (m *Manager) GetProblemPatternsYAML() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, err := yaml.Marshal(m.currentConfig.ProblemPatterns)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetScenariosYAML returns the current scenarios config as YAML.
func (m *Manager) GetScenariosYAML() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, err := yaml.Marshal(m.currentConfig.Scenarios)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *Manager) generateSummary() string {
	if len(m.pendingChanges) == 0 {
		return "No changes"
	}
	sections := make([]string, 0, len(m.pendingChanges))
	for _, change := range m.pendingChanges {
		sections = append(sections, string(change.Type))
	}
	if len(sections) == 1 {
		return "Updated " + sections[0]
	}
	return "Updated " + joinStrings(sections)
}

func joinStrings(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}
	result := strs[0]
	for i := 1; i < len(strs)-1; i++ {
		result += ", " + strs[i]
	}
	result += " and " + strs[len(strs)-1]
	return result
}

// deepCopyConfig creates a deep copy of a Config.
func deepCopyConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	// Use JSON marshaling for deep copy (simple and reliable)
	data, err := json.Marshal(cfg)
	if err != nil {
		// Fallback: return as-is (shouldn't happen with valid config)
		return cfg
	}
	var copy Config
	if err := json.Unmarshal(data, &copy); err != nil {
		return cfg
	}
	return &copy
}
