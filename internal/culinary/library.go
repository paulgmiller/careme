package culinary

import (
	"errors"
	"fmt"
	"strings"
)

type Module struct {
	ID            string
	RecipeRules   []string
	CritiqueRules []string
}

type Library struct {
	modules []Module
}

func NewLibrary(modules ...Module) (Library, error) {
	seen := make(map[string]struct{}, len(modules))
	normalized := make([]Module, 0, len(modules))
	for _, module := range modules {
		module.ID = strings.TrimSpace(module.ID)
		if module.ID == "" {
			return Library{}, errors.New("culinary guidance module ID is required")
		}
		if _, ok := seen[module.ID]; ok {
			return Library{}, fmt.Errorf("duplicate culinary guidance module %q", module.ID)
		}
		seen[module.ID] = struct{}{}

		var err error
		module.RecipeRules, err = normalizeRules(module.ID, "recipe", module.RecipeRules)
		if err != nil {
			return Library{}, err
		}
		module.CritiqueRules, err = normalizeRules(module.ID, "critique", module.CritiqueRules)
		if err != nil {
			return Library{}, err
		}
		if len(module.RecipeRules) == 0 && len(module.CritiqueRules) == 0 {
			return Library{}, fmt.Errorf("culinary guidance module %q has no rules", module.ID)
		}
		normalized = append(normalized, module)
	}
	return Library{modules: normalized}, nil
}

func DefaultLibrary() Library {
	library, err := NewLibrary(
		Salt(),
		Browning(),
		AcidTiming(),
	)
	if err != nil {
		panic(err)
	}
	return library
}

func (l Library) RecipePrompt() string {
	return l.prompt(func(module Module) []string {
		return module.RecipeRules
	})
}

func (l Library) CritiquePrompt() string {
	return l.prompt(func(module Module) []string {
		return module.CritiqueRules
	})
}

func (l Library) prompt(rulesFor func(Module) []string) string {
	var prompt strings.Builder
	wroteHeading := false
	for _, module := range l.modules {
		rules := rulesFor(module)
		if len(rules) == 0 {
			continue
		}
		if !wroteHeading {
			prompt.WriteString("\n\n# Culinary Guidance")
			wroteHeading = true
		}
		fmt.Fprintf(&prompt, "\n## %s\n", module.ID)
		for _, rule := range rules {
			fmt.Fprintf(&prompt, "- %s\n", rule)
		}
	}
	return strings.TrimRight(prompt.String(), "\n")
}

func normalizeRules(moduleID, kind string, rules []string) ([]string, error) {
	normalized := make([]string, 0, len(rules))
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			return nil, fmt.Errorf("culinary guidance module %q has an empty %s rule", moduleID, kind)
		}
		normalized = append(normalized, rule)
	}
	return normalized, nil
}
