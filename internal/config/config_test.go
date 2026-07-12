package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "arkannie.config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		absent  bool // true → no se escribe arkannie.config.yaml
		content string
		want    Config // Root se rellena en el subtest
		wantErr string // subcadena esperada en el error; "" → sin error
	}{
		{
			name:    "U0-T1_yaml_completo_valido",
			content: "timeout_default: 300\nmax_concurrency: 8\nclaude_bin: /opt/bin/claude\n",
			want:    Config{TimeoutDefault: 300, MaxConcurrency: 8, ClaudeBin: "/opt/bin/claude"},
		},
		{
			name:   "U0-T2_archivo_ausente_defaults",
			absent: true,
			want:   Config{TimeoutDefault: 120, MaxConcurrency: 4, ClaudeBin: "claude"},
		},
		{
			name:    "U0-T3_yaml_malformado_error_con_causa",
			content: "timeout_default: [1, 2\n",
			wantErr: "arkannie.config.yaml",
		},
		{
			name:    "U0-T4_valores_invalidos",
			content: "timeout_default: 0\nmax_concurrency: -1\n",
			wantErr: "timeout_default",
		},
		{
			name:    "max_concurrency_invalido",
			content: "max_concurrency: -1\n",
			wantErr: "max_concurrency",
		},
		{
			name:    "claves_desconocidas_ignoradas",
			content: "unknown_key: true\ntimeout_default: 60\n",
			want:    Config{TimeoutDefault: 60, MaxConcurrency: 4, ClaudeBin: "claude"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if !tt.absent {
				writeConfigFile(t, root, tt.content)
			}
			got, err := Load(root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, quiero que contenga %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error inesperado: %v", err)
			}
			tt.want.Root = root
			if *got != tt.want {
				t.Errorf("Load() = %+v, quiero %+v", *got, tt.want)
			}
		})
	}
}
