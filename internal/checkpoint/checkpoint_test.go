package checkpoint

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"

	"arkannie/internal/ram"
)

var iso8601UTC = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

func sampleBindings() map[string]ram.Value {
	return map[string]ram.Value{
		"report": {Kind: ram.KString, Str: "auth ok"},
		"files": {Kind: ram.KList, List: []ram.Value{
			{Kind: ram.KString, Str: "a.go"},
			{Kind: ram.KString, Str: "b.go"},
		}},
		"meta": {Kind: ram.KMap, Map: map[string]ram.Value{
			"owner": {Kind: ram.KString, Str: "arkannie"},
			"tags": {Kind: ram.KList, List: []ram.Value{
				{Kind: ram.KString, Str: "x"},
			}},
		}},
	}
}

func checkpointPath(memDir, sanitized string) string {
	return filepath.Join(memDir, "checkpoints", sanitized+"-latest.yaml")
}

func mustWrite(t *testing.T, memDir, program string, step int, snap map[string]ram.Value) {
	t.Helper()
	if err := Write(memDir, program, step, snap); err != nil {
		t.Fatalf("Write(%q) error inesperado: %v", program, err)
	}
}

func TestWriteLoad(t *testing.T) {
	t.Run("U5-T1_round_trip_string_list_map_anidado", func(t *testing.T) {
		dir := t.TempDir()
		snap := sampleBindings()
		mustWrite(t, dir, "deploy.ann", 2, snap)

		cp, ok := Load(dir, "deploy.ann")
		if !ok {
			t.Fatal("Load = false tras Write; el checkpoint debe existir")
		}
		if cp.Program != "deploy.ann" {
			t.Fatalf("Program = %q; quiero %q", cp.Program, "deploy.ann")
		}
		if !reflect.DeepEqual(cp.Bindings, snap) {
			t.Fatalf("bindings no idénticos tras round-trip:\n got: %+v\nwant: %+v", cp.Bindings, snap)
		}
	})

	t.Run("U5-T2_load_sin_checkpoint_false", func(t *testing.T) {
		cp, ok := Load(t.TempDir(), "inexistente.ann")
		if ok || cp != nil {
			t.Fatalf("Load sin checkpoint = (%+v, %v); quiero (nil, false)", cp, ok)
		}
	})

	t.Run("U5-T5_load_devuelve_last_completed_step_para_resume", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, dir, "resume.ann", 3, sampleBindings())
		cp, ok := Load(dir, "resume.ann")
		if !ok {
			t.Fatal("Load = false tras Write")
		}
		if cp.LastCompletedStep != 3 {
			t.Fatalf("LastCompletedStep = %d; quiero 3", cp.LastCompletedStep)
		}
		if resume := cp.LastCompletedStep + 1; resume != 4 {
			t.Fatalf("resume en step %d; quiero 4 (§10.4)", resume)
		}
	})
}

func TestPersistenciaYClean(t *testing.T) {
	t.Run("U5-T3_error_no_limpia_clean_borra", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, dir, "fail.ann", 1, sampleBindings())

		// Se simula un error del programa: el runtime NO llama Clean.
		// Ni Write ni Load deben haber borrado el archivo.
		path := checkpointPath(dir, "fail-ann")
		if _, ok := Load(dir, "fail.ann"); !ok {
			t.Fatal("el checkpoint debe persistir tras un error (§10.5)")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("el archivo del checkpoint debe persistir tras un error: %v", err)
		}

		if err := Clean(dir, "fail.ann"); err != nil {
			t.Fatalf("Clean error inesperado: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("Clean no borró el checkpoint: %v", err)
		}
		if _, ok := Load(dir, "fail.ann"); ok {
			t.Fatal("Load = true tras Clean; quiero false")
		}
	})

	t.Run("clean_sin_checkpoint_es_nil", func(t *testing.T) {
		if err := Clean(t.TempDir(), "nada.ann"); err != nil {
			t.Fatalf("Clean sin checkpoint = %v; quiero nil", err)
		}
	})

	t.Run("write_sobreescribe_checkpoint_del_mismo_programa", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, dir, "deploy.ann", 1, sampleBindings())
		mustWrite(t, dir, "deploy.ann", 5, sampleBindings())

		entries, err := os.ReadDir(filepath.Join(dir, "checkpoints"))
		if err != nil {
			t.Fatalf("leyendo dir de checkpoints: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("checkpoints en dir = %d; quiero 1 (un vigente por programa)", len(entries))
		}
		cp, ok := Load(dir, "deploy.ann")
		if !ok || cp.LastCompletedStep != 5 {
			t.Fatalf("Load tras sobreescritura = (%+v, %v); quiero step 5", cp, ok)
		}
	})
}

func TestFormatoYAML(t *testing.T) {
	t.Run("U5-T4_yaml_contiene_exactamente_los_campos_y_timestamp_iso8601", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, dir, "audit.ann", 0, sampleBindings())

		raw, err := os.ReadFile(checkpointPath(dir, "audit-ann"))
		if err != nil {
			t.Fatalf("leyendo el YAML escrito: %v", err)
		}
		var doc map[string]yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("el archivo no es YAML válido: %v", err)
		}
		want := []string{"program", "timestamp", "last_completed_step", "bindings"}
		if len(doc) != len(want) {
			t.Fatalf("el YAML tiene %d claves; quiero exactamente %d (%v)", len(doc), len(want), want)
		}
		for _, k := range want {
			if _, ok := doc[k]; !ok {
				t.Fatalf("falta la clave %q en el YAML", k)
			}
		}
		if ts := doc["timestamp"].Value; !iso8601UTC.MatchString(ts) {
			t.Fatalf("timestamp = %q; quiero ISO8601 UTC (ej. 2026-07-01T21:45:00Z)", ts)
		}
		if step := doc["last_completed_step"].Value; step != "0" {
			t.Fatalf("last_completed_step = %q; quiero \"0\"", step)
		}
	})
}

func TestSanitizacionYDirectorios(t *testing.T) {
	t.Run("sanitiza_nombre_de_programa_a_a-z0-9-", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, dir, "My Plan (v2).ann", 0, sampleBindings())
		path := checkpointPath(dir, "my-plan-v2-ann")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("archivo sanitizado no encontrado en %s: %v", path, err)
		}
		if _, ok := Load(dir, "My Plan (v2).ann"); !ok {
			t.Fatal("Load con el mismo nombre sin sanitizar debe encontrar el checkpoint")
		}
	})

	t.Run("nombre_sin_caracteres_validos_usa_fallback", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, dir, "¡¿!?", 0, sampleBindings())
		if _, err := os.Stat(checkpointPath(dir, "program")); err != nil {
			t.Fatalf("fallback de nombre no encontrado: %v", err)
		}
	})

	t.Run("load_con_yaml_corrupto_devuelve_false", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "checkpoints"), 0o755); err != nil {
			t.Fatal(err)
		}
		corrupt := []byte("program: [sin cerrar")
		if err := os.WriteFile(checkpointPath(dir, "roto-ann"), corrupt, 0o644); err != nil {
			t.Fatal(err)
		}
		if cp, ok := Load(dir, "roto.ann"); ok {
			t.Fatalf("Load con YAML corrupto = (%+v, true); quiero (nil, false)", cp)
		}
	})

	t.Run("write_crea_directorios_faltantes", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "anidado", ".mem")
		mustWrite(t, dir, "deep.ann", 0, sampleBindings())
		if _, ok := Load(dir, "deep.ann"); !ok {
			t.Fatal("Write debe crear los directorios que falten (0o755)")
		}
	})
}
