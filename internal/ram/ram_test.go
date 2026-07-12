package ram

import (
	"errors"
	"reflect"
	"testing"
)

func strVal(s string) Value { return Value{Kind: KString, Str: s} }

func listVal(items ...Value) Value { return Value{Kind: KList, List: items} }

func mapVal(m map[string]Value) Value { return Value{Kind: KMap, Map: m} }

func mustSet(t *testing.T, r *RAM, name string, v Value) {
	t.Helper()
	if err := r.Set(name, v); err != nil {
		t.Fatalf("Set(%q) error inesperado: %v", name, err)
	}
}

func TestScopeVisibility(t *testing.T) {
	t.Run("U4-T1_set_get_mismo_scope", func(t *testing.T) {
		r := New()
		mustSet(t, r, "x", strVal("hola"))
		got, ok := r.Get("x")
		if !ok {
			t.Fatal("Get(x) = false; el binding debe existir en el mismo scope")
		}
		if !reflect.DeepEqual(got, strVal("hola")) {
			t.Fatalf("Get(x) = %+v; quiero %+v", got, strVal("hola"))
		}
	})

	t.Run("U4-T2_outer_visible_en_inner", func(t *testing.T) {
		r := New()
		mustSet(t, r, "outer", strVal("visible"))
		r.Push()
		defer r.Pop()
		got, ok := r.Get("outer")
		if !ok {
			t.Fatal("Get(outer) = false; el binding externo debe verse en el scope interno (§4.2)")
		}
		if got.Str != "visible" {
			t.Fatalf("Get(outer).Str = %q; quiero %q", got.Str, "visible")
		}
	})

	t.Run("U4-T3_inner_invisible_tras_pop", func(t *testing.T) {
		r := New()
		r.Push()
		mustSet(t, r, "inner", strVal("efimero"))
		r.Pop()
		if _, ok := r.Get("inner"); ok {
			t.Fatal("Get(inner) = true tras Pop; los bindings del bloque deben destruirse (§4.2)")
		}
	})

	t.Run("U4-T4_scopes_hermanos_aislados", func(t *testing.T) {
		r := New()
		// Siblings de parallel simulados como Push/Set/Pop secuenciales.
		r.Push()
		mustSet(t, r, "a", strVal("del-hermano-1"))
		if _, ok := r.Get("a"); !ok {
			t.Fatal("Get(a) = false dentro del propio sub-bloque")
		}
		r.Pop()

		r.Push()
		if _, ok := r.Get("a"); ok {
			t.Fatal("el sub-bloque hermano ve el binding de su sibling (§4.2 lo prohíbe)")
		}
		mustSet(t, r, "b", strVal("del-hermano-2"))
		r.Pop()

		if _, ok := r.Get("b"); ok {
			t.Fatal("binding de sub-bloque visible en el scope externo tras Pop")
		}
	})

	t.Run("U4-T5_binding_each_vive_solo_esa_ejecucion", func(t *testing.T) {
		r := New()
		mustSet(t, r, "resultados", listVal(strVal("r1")))
		// Una ejecución del handler each -> : Push, binding local, Pop.
		r.Push()
		mustSet(t, r, "item", strVal("r1"))
		if got, ok := r.Get("item"); !ok || got.Str != "r1" {
			t.Fatalf("Get(item) = (%+v, %v) dentro de la ejecución del each", got, ok)
		}
		r.Pop()
		if _, ok := r.Get("item"); ok {
			t.Fatal("el binding del each sobrevivió a su ejecución (§4.2 lo prohíbe)")
		}
		if _, ok := r.Get("resultados"); !ok {
			t.Fatal("el binding externo debe seguir vivo tras el each")
		}
	})

	t.Run("U4-T6_get_inexistente_false", func(t *testing.T) {
		r := New()
		if v, ok := r.Get("no_existe"); ok {
			t.Fatalf("Get(no_existe) = (%+v, true); quiero (_, false)", v)
		}
	})
}

func TestSnapshot(t *testing.T) {
	t.Run("U4-T7_snapshot_outer_inner_shadowing", func(t *testing.T) {
		r := New()
		mustSet(t, r, "a", strVal("outer"))
		mustSet(t, r, "b", strVal("solo-outer"))
		r.Push()
		mustSet(t, r, "a", strVal("inner"))
		mustSet(t, r, "c", strVal("solo-inner"))

		got := r.Snapshot()
		want := map[string]Value{
			"a": strVal("inner"),
			"b": strVal("solo-outer"),
			"c": strVal("solo-inner"),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Snapshot() = %+v; quiero %+v (inner gana sobre outer)", got, want)
		}
	})

	t.Run("snapshot_devuelve_copias_profundas", func(t *testing.T) {
		r := New()
		mustSet(t, r, "m", mapVal(map[string]Value{"k": strVal("v")}))
		snap := r.Snapshot()
		snap["m"].Map["k"] = strVal("mutado")
		got, _ := r.Get("m")
		if got.Map["k"].Str != "v" {
			t.Fatalf("mutar el Snapshot alteró el valor almacenado: %+v", got)
		}
	})
}

func TestImmutability(t *testing.T) {
	t.Run("U4-T8_mutar_list_devuelta_no_altera_almacenado", func(t *testing.T) {
		r := New()
		nested := mapVal(map[string]Value{"k": strVal("v")})
		mustSet(t, r, "l", listVal(strVal("a"), strVal("b"), nested))

		got, ok := r.Get("l")
		if !ok {
			t.Fatal("Get(l) = false")
		}
		got.List[0] = strVal("mutado")
		got.List[2].Map["k"] = strVal("mutado-profundo")

		again, _ := r.Get("l")
		if again.List[0].Str != "a" {
			t.Fatalf("mutación superficial de la List devuelta alteró lo almacenado: %+v", again)
		}
		if again.List[2].Map["k"].Str != "v" {
			t.Fatalf("mutación profunda de la List devuelta alteró lo almacenado: %+v", again)
		}
	})

	t.Run("mutar_valor_tras_set_no_altera_almacenado", func(t *testing.T) {
		r := New()
		v := listVal(strVal("a"))
		mustSet(t, r, "l", v)
		v.List[0] = strVal("mutado")
		got, _ := r.Get("l")
		if got.List[0].Str != "a" {
			t.Fatalf("mutar el valor original tras Set alteró lo almacenado: %+v", got)
		}
	})
}

func TestSetNameValidation(t *testing.T) {
	tests := []struct {
		name    string
		binding string
		wantErr bool
	}{
		{"alfanumerico_valido", "abc123", false},
		{"underscore_valido", "my_var_2", false},
		{"solo_underscore_valido", "_", false},
		{"vacio_invalido", "", true},
		{"prefijo_dollar_invalido", "$x", true},
		{"espacio_invalido", "a b", true},
		{"guion_invalido", "a-b", true},
		{"unicode_invalido", "año", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			err := r.Set(tt.binding, strVal("v"))
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidName) {
					t.Fatalf("Set(%q) err = %v; quiero ErrInvalidName", tt.binding, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Set(%q) err = %v; quiero nil", tt.binding, err)
			}
		})
	}
}

func TestPopEnScopeRaiz(t *testing.T) {
	r := New()
	r.Pop() // no debe hacer panic ni destruir el scope raíz
	mustSet(t, r, "x", strVal("v"))
	if _, ok := r.Get("x"); !ok {
		t.Fatal("el scope raíz debe sobrevivir a un Pop de más")
	}
}
