// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// compiled parses the embedded schema once per test run.
func compiled(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(SchemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("aibench.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("aibench.schema.json")
	if err != nil {
		t.Fatalf("schema does not compile: %v", err)
	}
	return s
}

// validateYAML runs a yaml document against the schema.
func validateYAML(t *testing.T, s *jsonschema.Schema, raw []byte) error {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	// Round-trip through JSON: the validator wants json-shaped values.
	j, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	return s.Validate(doc)
}

// TestSchemaMatchesExampleAndLiveConfig pins the artifact against
// reality: the shipped example (and the developer's live config when
// present) must validate.
func TestSchemaMatchesExampleAndLiveConfig(t *testing.T) {
	s := compiled(t)
	for _, path := range []string{"../../examples/settings/aibench.yaml", "../../.aibench/aibench.yaml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			if strings.Contains(path, "examples/") {
				t.Errorf("shipped example missing: %v", err)
			}
			continue // the live config may not exist on CI
		}
		if err := validateYAML(t, s, raw); err != nil {
			t.Errorf("%s does not validate: %v", path, err)
		}
	}
}

// TestSchemaRejectsInvalid mirrors the strict parser's fail-loud rules.
func TestSchemaRejectsInvalid(t *testing.T) {
	s := compiled(t)
	cases := map[string]string{
		"unknown key":         "profiles:\n  p:\n    kind: model\n    api:\n      provider: openai\n      stroe: true\n",
		"bad kind":            "profiles:\n  p:\n    kind: evaluations\n",
		"inline prompt group": "profiles:\n  p:\n    kind: model\n    prompt:\n      file: x.md\n",
		"scalar starters":     "prompts:\n  s:\n    starters: one.md\nprofiles:\n  p:\n    kind: model\n",
		"agent + api.type":    "profiles:\n  p:\n    kind: agent\n    api:\n      type: responses\n",
		"agent + api.store":   "profiles:\n  p:\n    kind: agent\n    api:\n      store: true\n",
		"missing kind":        "profiles:\n  p:\n    api:\n      provider: openai\n",
	}
	for name, doc := range cases {
		if err := validateYAML(t, s, []byte(doc)); err == nil {
			t.Errorf("%s: validated; want rejection", name)
		}
	}
}

// TestSchemaExampleModeline pins the example config's editor wiring to
// the same published URL the scaffolds write — no schema copies ride the
// repo or any config folder.
func TestSchemaExampleModeline(t *testing.T) {
	raw, err := os.ReadFile("../../examples/settings/aibench.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "$schema="+SchemaURL) {
		t.Error("examples/settings/aibench.yaml modeline does not reference SchemaURL")
	}
}

// TestSchemaCoversStructTags is the struct↔schema drift guard: every
// yaml tag on the profile structs appears in the schema's corresponding
// $defs properties, and vice versa.
func TestSchemaCoversStructTags(t *testing.T) {
	var doc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(SchemaJSON, &doc); err != nil {
		t.Fatal(err)
	}

	tags := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := range rt.NumField() {
			tag := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]
			if tag != "" && tag != "-" {
				out = append(out, tag)
			}
		}
		return out
	}
	// Profile's group fields point at their own $defs entries; the
	// prompts axis has its own def.
	checks := map[string]any{
		"profile":   Profile{},
		"api":       ProfileAPI{},
		"auth":      ProfileAuth{},
		"promptset": PromptSet{},
	}
	for def, v := range checks {
		props := doc.Defs[def].Properties
		if props == nil {
			t.Fatalf("schema $defs.%s missing", def)
		}
		for _, tag := range tags(v) {
			if _, ok := props[tag]; !ok {
				t.Errorf("$defs.%s missing property %q (struct has it)", def, tag)
			}
		}
		structTags := tags(v)
		for prop := range props {
			found := false
			for _, tag := range structTags {
				if tag == prop {
					found = true
				}
			}
			if !found {
				t.Errorf("$defs.%s has property %q the struct lacks", def, prop)
			}
		}
	}
}

// TestSchemaURLModeline pins the editor wiring: the scaffold's modeline
// references the published schema at the artifacts repo's schema/ path.
func TestSchemaURLModeline(t *testing.T) {
	if !strings.HasSuffix(SchemaURL, "/schema/aibench.schema.json") {
		t.Errorf("SchemaURL = %q; want the artifacts repo's schema/ path", SchemaURL)
	}
	if !strings.Contains(schemaModeline, "$schema="+SchemaURL) {
		t.Errorf("scaffold modeline %q does not reference SchemaURL", schemaModeline)
	}
}
