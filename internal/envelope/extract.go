package envelope

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Extract pulls an envelope out of the stdout of a
// `claude -p --output-format json` run. Deterministic algorithm, in order:
// deserialize the outer JSON and take its string `result` field; try a
// direct YAML unmarshal of the result; on failure retry with the contents
// of the first fenced ``` block; otherwise fail with an extraction error.
// Raw is always populated with the complete result text.
func Extract(stdout []byte) (*Envelope, error) {
	var outer map[string]any
	if err := json.Unmarshal(stdout, &outer); err != nil {
		return nil, fmt.Errorf("extracting envelope: stdout is not valid claude JSON: %w", err)
	}
	result, ok := outer["result"].(string)
	if !ok {
		return nil, errors.New(`extracting envelope: claude JSON has no string "result" field`)
	}
	doc, ok := decodeYAMLMap(result)
	if !ok {
		if inner, found := fencedBlock(result); found {
			doc, ok = decodeYAMLMap(inner)
		}
	}
	if !ok {
		return nil, fmt.Errorf("extracting envelope: result contains no YAML map with an id key: %q", truncate(result, 200))
	}
	return fromDoc(doc, result), nil
}

// decodeYAMLMap unmarshals s as YAML and accepts the document only when it
// is a map that carries an `id` key (the spike-confirmed base case).
func decodeYAMLMap(s string) (map[string]any, bool) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		return nil, false
	}
	if doc == nil {
		return nil, false
	}
	if _, ok := doc["id"]; !ok {
		return nil, false
	}
	return doc, true
}

// fencedBlock returns the contents of the first ``` or ```yaml fenced block
// in s, skipping the fence info string up to the end of its line.
func fencedBlock(s string) (string, bool) {
	const fence = "```"
	start := strings.Index(s, fence)
	if start == -1 {
		return "", false
	}
	rest := s[start+len(fence):]
	nl := strings.IndexByte(rest, '\n')
	if nl == -1 {
		return "", false
	}
	body := rest[nl+1:]
	end := strings.Index(body, fence)
	if end == -1 {
		return "", false
	}
	return body[:end], true
}

// fromDoc maps a decoded YAML document onto an Envelope. Fields with
// unexpected types are left at their zero values so Validate (§2) reports
// the precise violation instead of Extract guessing.
func fromDoc(doc map[string]any, raw string) *Envelope {
	e := &Envelope{Raw: raw}
	if id, ok := doc["id"].(string); ok {
		e.ID = id
	}
	if st, ok := doc["status"].(string); ok {
		e.Status = Status(st)
	}
	if p, ok := doc["payload"]; ok {
		e.Payload = p // any: string, []any or map[string]any
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
