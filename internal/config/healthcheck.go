package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrClaudeUnavailable indica que el CLI de claude no puede ejecutarse.
// El runtime lo mapea a un error de Class B.
var ErrClaudeUnavailable = errors.New("claude CLI unavailable")

const (
	healthcheckFile   = "healthcheck.yaml"
	cacheTTL          = 24 * time.Hour
	versionCmdTimeout = 10 * time.Second
)

// healthCache es el schema de <memDir>/healthcheck.yaml.
type healthCache struct {
	Version   string `yaml:"version"`
	CheckedAt string `yaml:"checked_at"` // ISO8601 (RFC 3339)
}

// Check verifica que cfg.ClaudeBin responde a --version y devuelve la versión
// reportada, cacheándola en <memDir>/healthcheck.yaml. Un cache con menos de
// 24h evita re-ejecutar el binario mientras este siga siendo resoluble.
func Check(cfg *Config, memDir string) (string, error) {
	if v, ok := cachedVersion(cfg.ClaudeBin, filepath.Join(memDir, healthcheckFile)); ok {
		return v, nil
	}
	version, err := runVersion(cfg.ClaudeBin)
	if err != nil {
		return "", err
	}
	if err := writeCache(memDir, version); err != nil {
		return "", err
	}
	return version, nil
}

// cachedVersion devuelve la versión cacheada si el cache es fresco (<24h) y
// el binario sigue resoluble; cualquier cache ausente, ilegible o incompleto
// se trata como inexistente y provoca una re-ejecución.
func cachedVersion(bin, path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var hc healthCache
	if err := yaml.Unmarshal(data, &hc); err != nil || hc.Version == "" {
		return "", false
	}
	checkedAt, err := time.Parse(time.RFC3339, hc.CheckedAt)
	if err != nil || time.Since(checkedAt) >= cacheTTL {
		return "", false
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", false
	}
	return hc.Version, true
}

// runVersion ejecuta `<bin> --version` con args discretos y timeout de 10s.
func runVersion(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionCmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%w: running %s --version: %v", ErrClaudeUnavailable, bin, err)
	}
	version := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if version == "" {
		return "", fmt.Errorf("%w: %s --version produced no output", ErrClaudeUnavailable, bin)
	}
	return version, nil
}

func writeCache(memDir, version string) error {
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		return fmt.Errorf("creating healthcheck cache dir: %w", err)
	}
	data, err := yaml.Marshal(healthCache{
		Version:   version,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("encoding healthcheck cache: %w", err)
	}
	path := filepath.Join(memDir, healthcheckFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing healthcheck cache: %w", err)
	}
	return nil
}
