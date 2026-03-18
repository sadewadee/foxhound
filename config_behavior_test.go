package foxhound_test

// config_behavior_test.go — TDD tests for BehaviorConfig addition to Config.
//
// RED phase: these fail until BehaviorConfig is added to Config and
// applyDefaults sets Profile = "moderate".

import (
	"os"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
)

// TestConfig_BehaviorConfig_FieldExists verifies at compile time that Config
// has a Behavior field of type BehaviorConfig with a Profile string field.
func TestConfig_BehaviorConfig_FieldExists(t *testing.T) {
	cfg := foxhound.Config{}
	cfg.Behavior.Profile = "moderate"
	if cfg.Behavior.Profile != "moderate" {
		t.Errorf("Behavior.Profile: want %q, got %q", "moderate", cfg.Behavior.Profile)
	}
}

// TestBehaviorConfig_DefaultProfile_IsModerate verifies that LoadConfig (and
// thus applyDefaults) sets Behavior.Profile to "moderate" when the YAML file
// does not contain a behavior section.
func TestBehaviorConfig_DefaultProfile_IsModerate(t *testing.T) {
	// Write a minimal config YAML without a behavior key.
	yaml := `
hunt:
  domain: example.com
  walkers: 1
`
	f, err := os.CreateTemp("", "foxhound-test-*.yaml")
	if err != nil {
		t.Fatalf("creating temp config: %v", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(yaml); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	_ = f.Close()

	cfg, err := foxhound.LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Behavior.Profile != "moderate" {
		t.Errorf("Behavior.Profile default: want %q, got %q", "moderate", cfg.Behavior.Profile)
	}
}

// TestBehaviorConfig_YAMLRoundtrip verifies that a YAML config with an
// explicit behavior.profile value is parsed correctly.
func TestBehaviorConfig_YAMLRoundtrip(t *testing.T) {
	yaml := `
hunt:
  domain: example.com
  walkers: 1
behavior:
  profile: careful
`
	f, err := os.CreateTemp("", "foxhound-test-*.yaml")
	if err != nil {
		t.Fatalf("creating temp config: %v", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(yaml); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	_ = f.Close()

	cfg, err := foxhound.LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Behavior.Profile != "careful" {
		t.Errorf("Behavior.Profile: want %q, got %q", "careful", cfg.Behavior.Profile)
	}
}

// TestBehaviorConfig_AggressiveProfile verifies the "aggressive" value passes
// through LoadConfig unchanged.
func TestBehaviorConfig_AggressiveProfile(t *testing.T) {
	yaml := `
hunt:
  domain: test.com
behavior:
  profile: aggressive
`
	f, err := os.CreateTemp("", "foxhound-test-*.yaml")
	if err != nil {
		t.Fatalf("creating temp config: %v", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(yaml); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	_ = f.Close()

	cfg, err := foxhound.LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Behavior.Profile != "aggressive" {
		t.Errorf("Behavior.Profile: want %q, got %q", "aggressive", cfg.Behavior.Profile)
	}
}
