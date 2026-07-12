// Package config carga arkannie.config.yaml y verifica la disponibilidad del CLI de claude.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Defaults aplicados cuando arkannie.config.yaml no existe o no define la clave.
const (
	defaultTimeout        = 120
	defaultMaxConcurrency = 4
	defaultClaudeBin      = "claude"
)

// Config es la configuración operativa del runtime de arkannie.
type Config struct {
	TimeoutDefault int    // yaml: timeout_default — segundos por invocación de agente
	MaxConcurrency int    // yaml: max_concurrency — procesos claude simultáneos
	ClaudeBin      string // yaml: claude_bin — binario de claude
	Root           string // raíz de arkannie; la fija el caller, no viene del yaml
}

// fileConfig refleja el yaml; los punteros distinguen "clave ausente"
// (default) de "cero explícito" (error de validación).
type fileConfig struct {
	TimeoutDefault *int    `yaml:"timeout_default"`
	MaxConcurrency *int    `yaml:"max_concurrency"`
	ClaudeBin      *string `yaml:"claude_bin"`
}

// Load lee <root>/arkannie.config.yaml. Archivo ausente → defaults sin error.
// YAML malformado → error con causa. Claves desconocidas → ignoradas.
func Load(root string) (*Config, error) {
	cfg := &Config{
		TimeoutDefault: defaultTimeout,
		MaxConcurrency: defaultMaxConcurrency,
		ClaudeBin:      defaultClaudeBin,
		Root:           root,
	}
	path := filepath.Join(root, "arkannie.config.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.apply(&fc)
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating %s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) apply(fc *fileConfig) {
	if fc.TimeoutDefault != nil {
		c.TimeoutDefault = *fc.TimeoutDefault
	}
	if fc.MaxConcurrency != nil {
		c.MaxConcurrency = *fc.MaxConcurrency
	}
	if fc.ClaudeBin != nil && *fc.ClaudeBin != "" {
		c.ClaudeBin = *fc.ClaudeBin
	}
}

func (c *Config) validate() error {
	if c.TimeoutDefault <= 0 {
		return fmt.Errorf("timeout_default must be > 0, got %d", c.TimeoutDefault)
	}
	if c.MaxConcurrency <= 0 {
		return fmt.Errorf("max_concurrency must be > 0, got %d", c.MaxConcurrency)
	}
	return nil
}
