package scheduler

import (
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
)

// TestMapValueDotPathAndReturn covers T3.5: a map() literal whose value is a
// dotted $ref resolves into a KMap, and that KMap flows to [return] as a fenced
// YAML block in the report body (§2.6, v0.3 output indicator).
func TestMapValueDotPathAndReturn(t *testing.T) {
	s, st := newValState()
	st.returnCounts = map[string]int{}
	_ = st.ram.Set("r", ram.Value{Kind: ram.KMap, Map: map[string]ram.Value{"campo": kstr("deep")}})

	m := ann.MapLit{Entries: []ann.MapEntry{
		{Key: "kind", Val: strElem("v")},
		{Key: "num", Val: refElem("r.campo")},
	}}
	got := s.mapValue(st, m)
	if got.Kind != ram.KMap || len(got.Map) != 2 {
		t.Fatalf("mapValue = %#v, want KMap of 2", got)
	}
	if got.Map["kind"].Str != "v" || got.Map["num"].Str != "deep" {
		t.Errorf("map = %#v, want {kind:v num:deep}", got.Map)
	}

	_ = st.ram.Set("m", got)
	s.execReturn(st, &ann.Dispatch{Command: "return", Args: []string{"$m"}, ID: "out"})
	out := st.report.String()
	if !strings.Contains(out, "```yaml") || !strings.Contains(out, "kind: v") || !strings.Contains(out, "num: deep") {
		t.Fatalf("return report = %q, want a YAML block carrying kind and num", out)
	}
}

// TestMapValueUnresolvableOmitted covers the Class A path for map values: an
// entry whose value is an unresolvable $ref is omitted with a Class A notice,
// mirroring listValue (§2.6, v0.3).
func TestMapValueUnresolvableOmitted(t *testing.T) {
	s, st := newValState()
	m := ann.MapLit{Entries: []ann.MapEntry{
		{Key: "ok", Val: strElem("keep")},
		{Key: "bad", Val: refElem("missing")},
	}}
	got := s.mapValue(st, m)
	if len(got.Map) != 1 || got.Map["ok"].Str != "keep" {
		t.Fatalf("map = %#v, want only ok=keep", got.Map)
	}
	if len(s.Notices) != 1 || !strings.Contains(s.Notices[0], "[class A]") ||
		!strings.Contains(s.Notices[0], "missing") {
		t.Errorf("want one Class A notice naming 'missing', got %v", s.Notices)
	}
}
