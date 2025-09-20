package html

import (
	"careme/internal/config"
	"strings"
	"testing"
)

func TestClarityScript_WithProjectID(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test123"},
	}
	
	script := ClarityScript(cfg)
	scriptStr := string(script)
	
	if !strings.Contains(scriptStr, "test123") {
		t.Error("Script should contain project ID")
	}
	
	if !strings.Contains(scriptStr, "clarity.ms/tag/") {
		t.Error("Script should contain Clarity URL")
	}
	
	if !strings.Contains(scriptStr, "<script") {
		t.Error("Should return a script tag")
	}
}

func TestClarityScript_WithoutProjectID(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: ""},
	}
	
	script := ClarityScript(cfg)
	
	if script != "" {
		t.Error("Should return empty string when project ID is not set")
	}
}