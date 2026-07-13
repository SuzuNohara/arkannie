package dispatch

import (
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
)

// setMap binds name to a KMap in r, failing the test on an invalid name.
func setMap(t *testing.T, r *ram.RAM, name string, m map[string]ram.Value) {
	t.Helper()
	if err := r.Set(name, ram.Value{Kind: ram.KMap, Map: m}); err != nil {
		t.Fatal(err)
	}
}

// TestContextBlockDotAccess pins dot-access resolution in the context_block
// interpolation position (T-05, R2/R3): a $x.field token must interpolate the
// VALUE of the field, not the whole KMap and not the literal text. Deep paths
// walk nested maps; an unresolvable path is a Class B naming the base binding
// and the failing segment; a path that descends into a non-map is a Class B
// suggesting the dot be separated from the reference.
func TestContextBlockDotAccess(t *testing.T) {
	a := loadFixtureAgent(t)
	str := func(s string) ram.Value { return ram.Value{Kind: ram.KString, Str: s} }

	t.Run("T1.1_field_value_inlined_not_full_map", func(t *testing.T) {
		r := ram.New()
		setMap(t, r, "rec", map[string]ram.Value{"name": str("suzu"), "team": str("core")})
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"target": "src"}, Context: "review $rec.name now"}
		got := buildBlock(t, a, d, r)
		if !strings.Contains(got, "text: review suzu now") {
			t.Errorf("dotted field value not inlined; block:\n%s", got)
		}
		if strings.Contains(got, "team: core") {
			t.Errorf("full KMap must not be dumped for a dotted access; block:\n%s", got)
		}
		if strings.Contains(got, "rec.name") {
			t.Errorf("dotted token must be resolved, not left literal; block:\n%s", got)
		}
	})

	t.Run("T1.2_deep_path", func(t *testing.T) {
		r := ram.New()
		setMap(t, r, "rec", map[string]ram.Value{
			"addr": {Kind: ram.KMap, Map: map[string]ram.Value{"city": str("Springfield")}},
		})
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"target": "src"}, Context: "in $rec.addr.city today"}
		got := buildBlock(t, a, d, r)
		if !strings.Contains(got, "text: in Springfield today") {
			t.Errorf("deep dotted path not resolved; block:\n%s", got)
		}
	})

	t.Run("T1_dotted_list_keyed_by_last_segment", func(t *testing.T) {
		r := ram.New()
		setMap(t, r, "rec", map[string]ram.Value{
			"items": {Kind: ram.KList, List: []ram.Value{str("alpha"), str("beta")}},
		})
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"target": "src"}, Context: "use $rec.items"}
		got := buildBlock(t, a, d, r)
		if !strings.Contains(got, "items:") || !strings.Contains(got, "- alpha") {
			t.Errorf("dotted list value not added as a context field; block:\n%s", got)
		}
		if strings.Contains(got, "rec.items") {
			t.Errorf("dotted token must be resolved, not left literal; block:\n%s", got)
		}
	})

	t.Run("T1.6_missing_field_classB_names_base_and_segment", func(t *testing.T) {
		r := ram.New()
		setMap(t, r, "rec", map[string]ram.Value{"name": str("suzu")})
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"target": "src"}, Context: "check $rec.missing"}
		op, name := mustSelect(t, a, d)
		_, err := BuildContextBlock(op, name, d, r)
		pde := wantClassB(t, err)
		if !strings.Contains(pde.Msg, "rec") || !strings.Contains(pde.Msg, "missing") {
			t.Errorf("error must name base 'rec' and failing segment 'missing': %q", pde.Msg)
		}
	})

	t.Run("T1.7_non_map_classB_suggests_cut", func(t *testing.T) {
		r := ram.New()
		if err := r.Set("v", str("lit")); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"target": "src"}, Context: "pick $v.2"}
		op, name := mustSelect(t, a, d)
		_, err := BuildContextBlock(op, name, d, r)
		pde := wantClassB(t, err)
		if !strings.Contains(pde.Msg, "v") || !strings.Contains(pde.Msg, "separate") {
			t.Errorf("error must name 'v' and suggest separating the dot from the reference: %q", pde.Msg)
		}
	})

	t.Run("T1.8_plain_ref_unchanged", func(t *testing.T) {
		r := ram.New()
		if err := r.Set("module", str("auth")); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"target": "src"}, Context: "review $module now"}
		got := buildBlock(t, a, d, r)
		if !strings.Contains(got, "text: review auth now") {
			t.Errorf("plain (undotted) ref regression; block:\n%s", got)
		}
	})
}
