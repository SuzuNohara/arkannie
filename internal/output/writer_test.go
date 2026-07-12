package output

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	runIDWithLabelRe = regexp.MustCompile(`^\d{8}T\d{6}\.\d{6}Z(-[a-z0-9-]+)?$`)
	runIDBareRe      = regexp.MustCompile(`^\d{8}T\d{6}\.\d{6}Z$`)
)

func testTimes() (time.Time, time.Time) {
	started := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return started, started.Add(3 * time.Second)
}

func TestU12T1StatusFrontmatterAndExitCodes(t *testing.T) {
	started, finished := testTimes()
	tests := []struct {
		name     string
		status   string
		wantCode int
	}{
		{"U12-T1_success", "success", 0},
		{"U12-T1_error", "error", 1},
		{"U12-T1_info", "info", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "out") // no existe: Write debe crearlo
			runID := "20260701T120000.000001Z-" + tc.status
			res := Result{Status: tc.status, Body: "body text\n"}
			path, err := Write(dir, runID, "echo", "run it", res, started, finished)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading output: %v", err)
			}
			want := "---\n" +
				"id: " + runID + "\n" +
				"agent: echo\n" +
				"status: " + tc.status + "\n" +
				"started: 2026-07-01T12:00:00Z\n" +
				"finished: 2026-07-01T12:00:03Z\n" +
				"input: run it\n" +
				"---\n" +
				"body text\n"
			if got := string(data); got != want {
				t.Errorf("file content mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
			if got := ExitCode(tc.status); got != tc.wantCode {
				t.Errorf("ExitCode(%q) = %d, want %d", tc.status, got, tc.wantCode)
			}
		})
	}
	t.Run("U12-T1_unknown_status_exit_code", func(t *testing.T) {
		if got := ExitCode("weird"); got != 1 {
			t.Errorf("ExitCode(\"weird\") = %d, want 1", got)
		}
	})
}

func TestU12T2NewRunIDFormat(t *testing.T) {
	t.Run("U12-T2_without_label", func(t *testing.T) {
		id := NewRunID("")
		if !runIDBareRe.MatchString(id) {
			t.Errorf("NewRunID(\"\") = %q, does not match %s", id, runIDBareRe)
		}
	})
	t.Run("U12-T2_with_label_sanitized", func(t *testing.T) {
		id := NewRunID("Mi Label!")
		if !runIDWithLabelRe.MatchString(id) {
			t.Errorf("NewRunID(\"Mi Label!\") = %q, does not match %s", id, runIDWithLabelRe)
		}
		if !strings.HasSuffix(id, "-mi-label-") {
			t.Errorf("NewRunID(\"Mi Label!\") = %q, want suffix %q", id, "-mi-label-")
		}
	})
}

func TestU12T3CollisionSuffix(t *testing.T) {
	started, finished := testTimes()
	t.Run("U12-T3_newest_keeps_clean_name_previous_archived", func(t *testing.T) {
		dir := t.TempDir()
		id := "b01"
		clean := filepath.Join(dir, id+".md")
		if err := os.WriteFile(clean, []byte("existing"), 0o644); err != nil {
			t.Fatalf("seeding collision: %v", err)
		}
		res := Result{Status: "success", Body: "new body\n"}
		path, err := Write(dir, id, "echo", "in", res, started, finished)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := filepath.Base(path); got != id+".md" {
			t.Errorf("new run path = %q, want the clean name %q", got, id+".md")
		}
		// The previous file was moved to -1 with its original content intact.
		archived, err := os.ReadFile(filepath.Join(dir, id+"-1.md"))
		if err != nil {
			t.Fatalf("reading archived file: %v", err)
		}
		if string(archived) != "existing" {
			t.Errorf("archived file content = %q, want %q", archived, "existing")
		}
		// The clean name now holds the NEW run.
		data, err := os.ReadFile(clean)
		if err != nil {
			t.Fatalf("reading clean file: %v", err)
		}
		if !strings.Contains(string(data), "new body") {
			t.Errorf("clean name does not hold the new run:\n%s", data)
		}
	})
	t.Run("U12-T3_second_collision_archives_to_next_free_suffix", func(t *testing.T) {
		dir := t.TempDir()
		id := "b02"
		res := Result{Status: "success", Body: ""}
		// v1
		if _, err := Write(dir, id, "echo", "in", Result{Status: "success", Body: "v1\n"}, started, finished); err != nil {
			t.Fatalf("Write v1: %v", err)
		}
		// v2: archives v1 -> -1
		if _, err := Write(dir, id, "echo", "in", Result{Status: "success", Body: "v2\n"}, started, finished); err != nil {
			t.Fatalf("Write v2: %v", err)
		}
		// v3: archives v2 -> -2, v3 keeps clean name
		res = Result{Status: "success", Body: "v3\n"}
		if _, err := Write(dir, id, "echo", "in", res, started, finished); err != nil {
			t.Fatalf("Write v3: %v", err)
		}
		checks := map[string]string{
			id + ".md":   "v3\n",
			id + "-1.md": "v1\n",
			id + "-2.md": "v2\n",
		}
		for name, want := range checks {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			if !strings.Contains(string(data), want) {
				t.Errorf("%s = %q, want body containing %q", name, data, want)
			}
		}
	})
	t.Run("U12-T3_exhausted_suffixes_errors", func(t *testing.T) {
		dir := t.TempDir()
		id := "b03"
		names := []string{id + ".md"}
		for _, n := range []string{"-1", "-2", "-3", "-4", "-5", "-6", "-7", "-8", "-9", "-10"} {
			names = append(names, id+n+".md")
		}
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
				t.Fatalf("seeding %s: %v", n, err)
			}
		}
		res := Result{Status: "success", Body: "b\n"}
		if _, err := Write(dir, id, "echo", "in", res, started, finished); err == nil {
			t.Error("Write succeeded after 10 archived suffixes, want error")
		}
	})
}

func TestU12T4Sanitize(t *testing.T) {
	redactedInputs := []struct {
		name string
		in   string
	}{
		{"U12-T4_begin_key_block", "-----BEGIN RSA PRIVATE KEY-----\nMIIEvQIBADANBgkq\n-----END RSA PRIVATE KEY-----"},
		{"U12-T4_connection_string", "postgres://user:hunter2@db.example.com:5432/app"},
		{"U12-T4_jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpM"},
		{"U12-T4_sk_prefix", "sk-abcdefghijklmnop1234"},
		{"U12-T4_ghp_prefix", "ghp_abcdefghijklmnop1234"},
		{"U12-T4_hex_64_chars", strings.Repeat("a1b2", 16)},
	}
	for _, tc := range redactedInputs {
		t.Run(tc.name, func(t *testing.T) {
			out := Sanitize("before " + tc.in + " after")
			if !strings.Contains(out, redactionMarker) {
				t.Errorf("Sanitize did not redact %s:\n%s", tc.name, out)
			}
			if strings.Contains(out, tc.in) {
				t.Errorf("credential survived sanitization:\n%s", out)
			}
		})
	}
	t.Run("U12-T4_normal_text_intact", func(t *testing.T) {
		normal := "# Reporte\n" +
			"```go\n" +
			"func main() {\n" +
			"\turl := \"https://example.com/path\"\n" +
			"\tfmt.Println(url)\n" +
			"}\n" +
			"```\n" +
			"```yaml\n" +
			"agent: echo\n" +
			"model: sonnet\n" +
			"timeout: 30\n" +
			"```\n"
		if got := Sanitize(normal); got != normal {
			t.Errorf("normal text was modified:\ngot:\n%s\nwant:\n%s", got, normal)
		}
	})
}

func TestU12T5CredentialNotPersisted(t *testing.T) {
	started, finished := testTimes()
	t.Run("U12-T5_body_credential_absent_from_file", func(t *testing.T) {
		dir := t.TempDir()
		secret := "ghp_supersecrettoken12345"
		res := Result{Status: "success", Body: "token: " + secret + "\n"}
		path, err := Write(dir, NewRunID("leak"), "echo", "in", res, started, finished)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		if strings.Contains(string(data), secret) {
			t.Errorf("written file contains the credential:\n%s", data)
		}
		if !strings.Contains(string(data), redactionMarker) {
			t.Errorf("written file lacks redaction marker:\n%s", data)
		}
	})
}
