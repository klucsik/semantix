// Package simulation provides anomaly injection for chaos scenarios.
package simulation

import (
	"math/rand"
	"sync"
	"time"

	"github.com/mreider/semantix/internal/config"
)

// AnomalyState tracks active anomalies for an endpoint.
type AnomalyState struct {
	Type      string
	Active    bool
	StartTime time.Time
	EndTime   time.Time
	Params    map[string]float64
}

// AnomalyManager manages chaos/anomaly injection.
type AnomalyManager struct {
	states map[string]*AnomalyState // key: "service.endpoint"
	rng    *rand.Rand
	mu     sync.RWMutex
}

// NewAnomalyManager creates a new anomaly manager.
func NewAnomalyManager(seed int64) *AnomalyManager {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &AnomalyManager{
		states: make(map[string]*AnomalyState),
		rng:    rand.New(rand.NewSource(seed)),
	}
}

// CheckAndApplyAnomalies checks if anomalies should be triggered and returns modified parameters.
func (am *AnomalyManager) CheckAndApplyAnomalies(
	serviceName string,
	endpointName string,
	anomalies []config.AnomalyConfig,
	baseLatencyMs float64,
	baseErrorRate float64,
) (latencyMs float64, errorRate float64) {
	key := serviceName + "." + endpointName
	latencyMs = baseLatencyMs
	errorRate = baseErrorRate

	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now()

	// Check if there's an active anomaly
	state, exists := am.states[key]
	if exists && state.Active {
		// Check if anomaly has expired
		if now.After(state.EndTime) {
			state.Active = false
			delete(am.states, key)
		} else {
			// Apply active anomaly
			switch state.Type {
			case "latency_spike":
				latencyMs = baseLatencyMs * state.Params["multiplier"]
			case "error_burst":
				errorRate = state.Params["error_rate"]
			case "latency_degradation":
				latencyMs = baseLatencyMs * state.Params["multiplier"]
			}
			return latencyMs, errorRate
		}
	}

	// Check if we should start a new anomaly
	for _, anomaly := range anomalies {
		if am.rng.Float64() < anomaly.Probability {
			duration, err := time.ParseDuration(anomaly.Duration)
			if err != nil {
				duration = 5 * time.Minute // default
			}

			state := &AnomalyState{
				Type:      anomaly.Type,
				Active:    true,
				StartTime: now,
				EndTime:   now.Add(duration),
				Params:    make(map[string]float64),
			}

			switch anomaly.Type {
			case "latency_spike":
				state.Params["multiplier"] = anomaly.Multiplier
				latencyMs = baseLatencyMs * anomaly.Multiplier
			case "error_burst":
				state.Params["error_rate"] = anomaly.ErrorRate
				errorRate = anomaly.ErrorRate
			case "latency_degradation":
				state.Params["multiplier"] = anomaly.Multiplier
				latencyMs = baseLatencyMs * anomaly.Multiplier
			}

			am.states[key] = state
			break // Only one anomaly at a time per endpoint
		}
	}

	return latencyMs, errorRate
}

// GetActiveAnomalies returns a list of currently active anomalies.
func (am *AnomalyManager) GetActiveAnomalies() []string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var active []string
	now := time.Now()
	for key, state := range am.states {
		if state.Active && now.Before(state.EndTime) {
			active = append(active, key+":"+state.Type)
		}
	}
	return active
}

// ScenarioManager handles time-based traffic scenarios.
type ScenarioManager struct {
	scenarios []config.ScenarioConfig
	rng       *rand.Rand
	mu        sync.RWMutex
}

// NewScenarioManager creates a new scenario manager.
func NewScenarioManager(scenarios []config.ScenarioConfig, seed int64) *ScenarioManager {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &ScenarioManager{
		scenarios: scenarios,
		rng:       rand.New(rand.NewSource(seed)),
	}
}

// GetTrafficMultiplier returns the current traffic multiplier based on active scenarios.
// For now, this is a simplified implementation that could be extended with cron parsing.
func (sm *ScenarioManager) GetTrafficMultiplier() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	now := time.Now()
	hour := now.Hour()
	weekday := now.Weekday()

	// Simple time-based matching (a full implementation would parse cron expressions)
	for _, scenario := range sm.scenarios {
		if sm.matchesSchedule(scenario.Schedule, hour, weekday) {
			return scenario.TrafficMultiplier
		}
	}

	return 1.0 // default multiplier
}

// matchesSchedule does simple schedule matching (simplified cron-like).
// Format: "0 HOURS * * DAYS" where HOURS is a range like "9-17" and DAYS is like "MON-FRI"
func (sm *ScenarioManager) matchesSchedule(schedule string, hour int, weekday time.Weekday) bool {
	// This is a simplified implementation
	// A full implementation would use a cron parser library

	switch {
	case contains(schedule, "MON-FRI") && (weekday >= time.Monday && weekday <= time.Friday):
		if contains(schedule, "9-17") && hour >= 9 && hour <= 17 {
			return true
		}
		if contains(schedule, "0-8,18-23") && (hour <= 8 || hour >= 18) {
			return true
		}
	case contains(schedule, "SAT,SUN") && (weekday == time.Saturday || weekday == time.Sunday):
		return true
	}

	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
