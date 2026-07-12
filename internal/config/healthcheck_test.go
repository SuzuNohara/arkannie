package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// writeStub escribe un script sh ejecutable en un t.TempDir() y devuelve su ruta.
func writeStub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude-stub")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readCacheFile(t *testing.T, memDir string) healthCache {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(memDir, "healthcheck.yaml"))
	if err != nil {
		t.Fatalf("leyendo cache: %v", err)
	}
	var hc healthCache
	if err := yaml.Unmarshal(data, &hc); err != nil {
		t.Fatalf("cache no es yaml valido: %v", err)
	}
	return hc
}

func TestCheck(t *testing.T) {
	t.Run("R18-a_stub_ok_version_y_cache", func(t *testing.T) {
		bin := writeStub(t, "#!/bin/sh\necho '1.2.3 (stub)'\n")
		memDir := filepath.Join(t.TempDir(), "mem", "nested") // no existe: Check debe crearlo
		version, err := Check(&Config{ClaudeBin: bin}, memDir)
		if err != nil {
			t.Fatalf("Check() error: %v", err)
		}
		if version != "1.2.3 (stub)" {
			t.Errorf("version = %q, quiero %q", version, "1.2.3 (stub)")
		}
		hc := readCacheFile(t, memDir)
		if hc.Version != "1.2.3 (stub)" {
			t.Errorf("cache version = %q, quiero %q", hc.Version, "1.2.3 (stub)")
		}
		if _, err := time.Parse(time.RFC3339, hc.CheckedAt); err != nil {
			t.Errorf("checked_at no es ISO8601: %q (%v)", hc.CheckedAt, err)
		}
	})

	t.Run("R18-b_stub_exit_1", func(t *testing.T) {
		bin := writeStub(t, "#!/bin/sh\nexit 1\n")
		_, err := Check(&Config{ClaudeBin: bin}, t.TempDir())
		if !errors.Is(err, ErrClaudeUnavailable) {
			t.Fatalf("err = %v, quiero ErrClaudeUnavailable", err)
		}
	})

	t.Run("R18-c_binario_inexistente", func(t *testing.T) {
		bin := filepath.Join(t.TempDir(), "no-such-claude")
		_, err := Check(&Config{ClaudeBin: bin}, t.TempDir())
		if !errors.Is(err, ErrClaudeUnavailable) {
			t.Fatalf("err = %v, quiero ErrClaudeUnavailable", err)
		}
	})

	t.Run("R18-d_cache_fresco_no_reejecuta", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "invoked")
		script := "#!/bin/sh\nif [ -f '" + marker + "' ]; then exit 1; fi\ntouch '" + marker + "'\necho '9.9.9'\n"
		bin := writeStub(t, script)
		memDir := t.TempDir()
		cfg := &Config{ClaudeBin: bin}
		for i := 1; i <= 2; i++ {
			version, err := Check(cfg, memDir)
			if err != nil {
				t.Fatalf("Check() llamada %d error: %v (el stub falla si se ejecuta dos veces)", i, err)
			}
			if version != "9.9.9" {
				t.Errorf("llamada %d: version = %q, quiero %q", i, version, "9.9.9")
			}
		}
	})

	t.Run("cache_viejo_se_reejecuta", func(t *testing.T) {
		bin := writeStub(t, "#!/bin/sh\necho 'new-version'\n")
		memDir := t.TempDir()
		stale, err := yaml.Marshal(healthCache{
			Version:   "old-version",
			CheckedAt: time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(memDir, "healthcheck.yaml"), stale, 0o644); err != nil {
			t.Fatal(err)
		}
		version, err := Check(&Config{ClaudeBin: bin}, memDir)
		if err != nil {
			t.Fatalf("Check() error: %v", err)
		}
		if version != "new-version" {
			t.Errorf("version = %q, quiero %q (cache viejo debe re-ejecutar)", version, "new-version")
		}
	})
}
