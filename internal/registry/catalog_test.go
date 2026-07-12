package registry

import (
	"strings"
	"testing"
)

// catalogRegistry loads a two-agent pool ([card] and [good]) for the catalog
// tests. capYAML and validAgentYAML both carry valid capabilities cards.
func catalogRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	writeAgent(t, dir, "card", capYAML, true)
	writeAgent(t, dir, "good", validAgentYAML, true)
	reg, errs := Load(dir)
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	return reg
}

func TestCatalogPool(t *testing.T) {
	reg := catalogRegistry(t)
	out, ok := reg.Catalog("")
	if !ok {
		t.Fatal(`Catalog("") ok=false, want true`)
	}
	for _, want := range []string{
		"AGENT CATALOG (2 agent(s))",
		"[card]", "[good]",
		"Turn a raw requirement into a structured brief.", // card purpose
		"use when:",                // label
		"run — Produce the brief.", // card operation + description
		"[grants: read]",
		"examples:",
		"[card] : reduce checkout abandonment", // card example
	} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog missing %q\n---\n%s", want, out)
		}
	}
	// Sorted by command token: [card] before [good].
	if strings.Index(out, "[card]") > strings.Index(out, "[good]") {
		t.Errorf("catalog not sorted by command:\n%s", out)
	}
}

func TestCatalogDeterministic(t *testing.T) {
	reg := catalogRegistry(t)
	a, _ := reg.Catalog("")
	b, _ := reg.Catalog("")
	if a != b {
		t.Errorf("catalog not deterministic:\n---A---\n%s\n---B---\n%s", a, b)
	}
}

func TestCatalogSingleAgent(t *testing.T) {
	reg := catalogRegistry(t)
	out, ok := reg.Catalog("card")
	if !ok {
		t.Fatal(`Catalog("card") ok=false, want true`)
	}
	if !strings.Contains(out, "[card]") {
		t.Errorf("single-agent catalog missing [card]:\n%s", out)
	}
	if strings.Contains(out, "[good]") {
		t.Errorf("single-agent catalog should not contain [good]:\n%s", out)
	}
	if !strings.Contains(out, "AGENT CATALOG (1 agent(s))") {
		t.Errorf("single-agent catalog count wrong:\n%s", out)
	}
	// Bracketed form resolves to the same card.
	brk, ok := reg.Catalog("[card]")
	if !ok || brk != out {
		t.Errorf("Catalog(\"[card]\") != Catalog(\"card\")")
	}
}

func TestCatalogNotFound(t *testing.T) {
	reg := catalogRegistry(t)
	if _, ok := reg.Catalog("nope"); ok {
		t.Error(`Catalog("nope") ok=true, want false`)
	}
}
