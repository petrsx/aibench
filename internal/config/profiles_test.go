// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleYAML = `# gateways
profiles:
  dev:
    kind: model
    api:
      provider: openai
      base: https://dev.example.net
      model: gpt-5.4-mini
    auth:
      credentials: %s
  staging:
    kind: model
    api:
      provider: openai
      base: https://staging.example.net
`

func writeSample(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "dev.env")
	if err := os.WriteFile(envPath, []byte("OPENAI_API_KEY=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "aibench.yaml")
	// Config-relative credentials path: resolution against the config's
	// directory is exactly what a user-level config needs.
	yaml := strings.ReplaceAll(sampleYAML, "%s", "dev.env")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestLoadFileAndResolve(t *testing.T) {
	cfgPath := writeSample(t)
	// Config paths resolve against the working directory; stand where
	// this test's files are.
	t.Chdir(filepath.Dir(cfgPath))
	f, ok, err := LoadFile(cfgPath)
	if err != nil || !ok {
		t.Fatalf("LoadFile() = ok %v, err %v; want ok", ok, err)
	}
	if len(f.Profiles) != 2 {
		t.Fatalf("File = %+v; want 2 profiles", f)
	}

	t.Setenv("OPENAI_API_KEY", "from-process")
	cfg, err := f.Resolve("dev")
	if err != nil {
		t.Fatalf("Resolve(dev) error = %v", err)
	}
	if cfg.APIBase != "https://dev.example.net" || cfg.Model != "gpt-5.4-mini" {
		t.Errorf("resolved = %+v; want profile endpoint/model", cfg)
	}
	if cfg.APIKey != "from-file" {
		t.Errorf("APIKey = %q; want the credentials file to beat the process env", cfg.APIKey)
	}

	// staging has no credentials file: process env fills the gap.
	cfg, err = f.Resolve("staging")
	if err != nil {
		t.Fatalf("Resolve(staging) error = %v", err)
	}
	if cfg.APIKey != "from-process" {
		t.Errorf("APIKey = %q; want process env fallback", cfg.APIKey)
	}

	if _, err := f.Resolve("nope"); err == nil {
		t.Error("Resolve(nope) = nil error; want unknown profile")
	}
}

// TestResolveProviderAPIProfiles pins the provider/api matrix in yaml: the
// provider defaults its api (openai → chat, anthropic → messages), the
// anthropic key falls back to the AZURE_API_KEY a Foundry deployment
// issues, and an api the provider doesn't speak is rejected.
func TestResolveProviderAPIProfiles(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "creds.env")
	if err := os.WriteFile(envPath, []byte("OPENAI_API_KEY=oai-key\nAZURE_API_KEY=foundry-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := "profiles:\n" +
		"  chat-default:\n    kind: model\n    api:\n      provider: openai\n      base: https://gw.example.net\n    auth:\n      credentials: creds.env\n" +
		"  claude-foundry:\n    kind: model\n    api:\n      provider: anthropic\n      base: https://res.services.ai.azure.com/anthropic\n    auth:\n      credentials: creds.env\n" +
		"  mismatched:\n    kind: model\n    api:\n      provider: anthropic\n      type: chat\n      base: https://api.anthropic.com\n    auth:\n      credentials: creds.env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// Config paths resolve against the working directory; stand where
	// this test's files are.
	t.Chdir(filepath.Dir(cfgPath))
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	chat, err := f.Resolve("chat-default")
	if err != nil {
		t.Fatalf("Resolve(chat-default) error = %v", err)
	}
	if chat.API != APIChat {
		t.Errorf("chat-default api = %q; want chat", chat.API)
	}

	foundry, err := f.Resolve("claude-foundry")
	if err != nil {
		t.Fatalf("Resolve(claude-foundry) error = %v", err)
	}
	if foundry.API != APIMessages {
		t.Errorf("foundry api = %q; want messages", foundry.API)
	}
	if foundry.APIKey != "foundry-key" {
		t.Errorf("foundry APIKey = %q; want AZURE_API_KEY value", foundry.APIKey)
	}

	if _, err := f.Resolve("mismatched"); err == nil || !strings.Contains(err.Error(), "anthropic provider") {
		t.Errorf("Resolve(mismatched) error = %v; want anthropic-provider api mismatch", err)
	}
}

// TestResolveHeadersBlockAmbientTokenAuth pins the auth ladder's guard: a
// profile that declares auth headers takes token auth only from its own
// credentials file — a service principal exported in the shell for
// unrelated Azure work must not ride a stealth Bearer token alongside the
// declared header. `credentials: env` profiles keep it: the process env
// is their declared source.
func TestResolveHeadersBlockAmbientTokenAuth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gw.env"), []byte("GW_KEY=s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	optin := "GW_KEY=s3cret\nAZURE_TENANT_ID=t\nAZURE_CLIENT_ID=c\nAZURE_CLIENT_SECRET=own\n"
	if err := os.WriteFile(filepath.Join(dir, "optin.env"), []byte(optin), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := "profiles:\n" +
		"  gw:\n    kind: model\n    api:\n      provider: openai\n      base: https://gw.example.net\n      headers:\n        api-key: ${GW_KEY}\n    auth:\n      credentials: gw.env\n" +
		"  optin:\n    kind: model\n    api:\n      provider: openai\n      base: https://gw.example.net\n      headers:\n        api-key: ${GW_KEY}\n    auth:\n      credentials: optin.env\n" +
		"  envsource:\n    kind: model\n    api:\n      provider: openai\n      base: https://gw.example.net\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// ambient shell state: a service principal for unrelated Azure work
	t.Setenv("AZURE_TENANT_ID", "ambient")
	t.Setenv("AZURE_CLIENT_ID", "ambient")
	t.Setenv("AZURE_CLIENT_SECRET", "ambient")
	t.Setenv("OPENAI_API_KEY", "")
	// Config paths resolve against the working directory; stand where
	// this test's files are.
	t.Chdir(filepath.Dir(cfgPath))
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg, err := f.Resolve("gw"); err != nil || cfg.HasServicePrincipal() {
		t.Errorf("gw secret = %q, %v; want the ambient service principal blocked by declared headers", cfg.ClientSecret, err)
	}
	if cfg, err := f.Resolve("optin"); err != nil || cfg.ClientSecret != "own" {
		t.Errorf("optin secret = %q, %v; want the credentials file's own service principal honored", cfg.ClientSecret, err)
	}
	if cfg, err := f.Resolve("envsource"); err != nil || cfg.ClientSecret != "ambient" {
		t.Errorf("envsource secret = %q, %v; want credentials: env to keep the process value", cfg.ClientSecret, err)
	}
}

// TestResolveStoreNeedsResponses pins the invalid-combination guard: a
// profile flipping store: true on an api type without server-side state
// fails resolve with an error naming the wrong pair — never a silently
// dropped knob.
func TestResolveStoreNeedsResponses(t *testing.T) {
	dir := t.TempDir()
	yaml := "profiles:\n" +
		"  chat:\n    kind: model\n    api:\n      provider: openai\n      type: chat\n      store: true\n      base: https://gw.example.net\n    auth:\n      credentials: env\n" +
		"  claude:\n    kind: model\n    api:\n      provider: anthropic\n      store: true\n      base: https://api.anthropic.com\n    auth:\n      credentials: env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("ANTHROPIC_API_KEY", "k")
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"chat", "claude"} {
		if _, err := f.Resolve(name); err == nil || !strings.Contains(err.Error(), "store: true needs api type responses") {
			t.Errorf("Resolve(%s) error = %v; want the store/api-type mismatch named", name, err)
		}
	}
}

func TestLoadFileMissing(t *testing.T) {
	_, ok, err := LoadFile(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil || ok {
		t.Errorf("LoadFile(missing) = ok %v, err %v; want ok=false, nil", ok, err)
	}
}

// TestLoadFileStrict pins the fail-loud parse: an unknown or misspelled
// key — including a leftover `active:`, which moved to the state file —
// is a named error, never a silently dropped knob. An empty file still
// loads (no profiles).
func TestLoadFileStrict(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := map[string]string{
		"unknown-top":  "active: dev\nprofiles: {}\n", // active lives in the state file now
		"typoed-field": "profiles:\n  dev:\n    kind: model\n    api:\n      provider: openai\n      base: https://x\n      stroe: true\n",
		"stray-prompt": "profiles:\n  dev:\n    kind: model\n    promt:\n      file: p.md\n    api:\n      provider: openai\n      base: https://x\n",
	}
	for name, content := range cases {
		if _, _, err := LoadFile(write(name+".yaml", content)); err == nil {
			t.Errorf("%s: LoadFile = nil error; want the unknown key rejected", name)
		}
	}

	if f, ok, err := LoadFile(write("empty.yaml", "")); err != nil || !ok || len(f.Profiles) != 0 {
		t.Errorf("empty file: ok=%v err=%v profiles=%d; want ok, nil, none", ok, err, len(f.Profiles))
	}
}

// TestPromptSets pins the prompts axis: ApplyPrompt resolves the set's
// files against the config's directory, a pin naming a missing set is a
// named error, an agent may take only a starters-only set, and the
// deleted inline prompt group fails the strict parse.
func TestPromptSets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	yaml := "tools:\n" +
		"  weather-api: weather.tools.json\n" +
		"  websearch: search.tools.json\n" +
		"prompts:\n" +
		"  weather:\n    instructions: assistant.md\n    starters:\n      - a.starters.md\n      - b.starters.md\n    tools:\n      - weather-api\n      - websearch\n" +
		"  bare-starters:\n    starters:\n      - a.starters.md\n" +
		"profiles:\n" +
		"  dev:\n    kind: model\n    api:\n      provider: openai\n      base: https://dev.example.net\n    prompt: weather\n" +
		"  agent:\n    kind: agent\n    api:\n      base: https://gw/agents/a\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n    prompt: bare-starters\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "k")
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := f.Resolve("dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ApplyPrompt(&cfg, "weather"); err != nil {
		t.Fatalf("ApplyPrompt(weather) error = %v", err)
	}
	// A set's content is taken as written: it resolves against the working
	// directory, so the path means what it would mean typed at a shell.
	if cfg.PromptFile != "assistant.md" {
		t.Errorf("PromptFile = %q; want it as written, resolved against cwd", cfg.PromptFile)
	}
	if len(cfg.StartersFiles) != 2 || cfg.StartersFiles[1] != "b.starters.md" {
		t.Errorf("StartersFiles = %v; want both as written", cfg.StartersFiles)
	}
	if len(cfg.ToolsFiles) != 2 || cfg.ToolsFiles[0] != "weather.tools.json" {
		t.Errorf("ToolsFiles = %v; want both keys in list order, as written", cfg.ToolsFiles)
	}

	// An unknown tools key is a named error listing what exists.
	broken := f
	set := broken.Prompts["weather"]
	set.Tools = []string{"nope"}
	broken.Prompts = map[string]PromptSet{"weather": set}
	if err := broken.ApplyPrompt(&cfg, "weather"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown tools key error = %v; want it named", err)
	}

	// Unknown set: a named error, never a silent no-prompt.
	if err := f.ApplyPrompt(&cfg, "nope"); err == nil {
		t.Error("ApplyPrompt(nope) = nil; want a named error")
	}

	// Agents own instructions server-side: a set carrying them is
	// rejected, a starters-only set is fine.
	agentCfg, err := f.Resolve("agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ApplyPrompt(&agentCfg, "weather"); err == nil {
		t.Error("agent + instructions set accepted; want rejection")
	}
	if err := f.ApplyPrompt(&agentCfg, "bare-starters"); err != nil || len(agentCfg.StartersFiles) != 1 {
		t.Errorf("agent + starters-only set: err=%v files=%v; want accepted", err, agentCfg.StartersFiles)
	}

	// PromptFiles feeds the watchers: every set's files plus every tool
	// file, resolved and deduped (a.starters.md appears in two sets).
	if files := f.PromptFiles(); len(files) != 5 {
		t.Errorf("PromptFiles = %v; want 5 unique declared files", files)
	}

	// The old inline prompt group is gone: it fails the strict parse.
	inline := "profiles:\n  dev:\n    kind: model\n    api:\n      provider: openai\n      base: https://x\n    prompt:\n      file: x.md\n"
	inlinePath := filepath.Join(dir, "inline.yaml")
	if err := os.WriteFile(inlinePath, []byte(inline), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadFile(inlinePath); err == nil {
		t.Error("inline prompt group parsed; want the strict parser to reject it")
	}
	// So does a scalar starters value — the list is the one spelling.
	scalar := "prompts:\n  p:\n    starters: one.md\nprofiles:\n  dev:\n    kind: model\n    api:\n      provider: openai\n      base: https://x\n"
	scalarPath := filepath.Join(dir, "scalar.yaml")
	if err := os.WriteFile(scalarPath, []byte(scalar), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadFile(scalarPath); err == nil {
		t.Error("scalar starters parsed; want the strict parser to reject it")
	}
}

// TestResolveKind pins the kind axis: it is always declared (missing or
// unknown kinds fail with named errors), and kind: agent implies the
// responses wire + server-side state, owns its instructions (no client
// prompt file), and rejects the implied/foreign knobs loudly.
func TestResolveKind(t *testing.T) {
	dir := t.TempDir()
	yaml := "profiles:\n" +
		"  agent:\n    kind: agent\n    api:\n      base: https://gw.example.net/agents/assistant\n      model: gpt-5.4-mini\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n" +
		"  nokind:\n    api:\n      provider: openai\n      base: https://gw.example.net\n    auth:\n      credentials: env\n" +
		"  badkind:\n    kind: evaluations\n    api:\n      provider: openai\n      base: https://gw.example.net\n    auth:\n      credentials: env\n" +
		"  agent-type:\n    kind: agent\n    api:\n      type: responses\n      base: https://gw.example.net\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n" +
		"  agent-store:\n    kind: agent\n    api:\n      store: true\n      base: https://gw.example.net\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n" +
		"  agent-anthropic:\n    kind: agent\n    api:\n      provider: anthropic\n      base: https://gw.example.net\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "k")
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	agent, err := f.Resolve("agent")
	if err != nil {
		t.Fatalf("Resolve(agent) error = %v", err)
	}
	if agent.Kind != KindAgent || agent.API != APIResponses || !agent.Store {
		t.Errorf("agent = kind %q api %q store %v; want agent/responses/true", agent.Kind, agent.API, agent.Store)
	}
	if agent.PromptFile != "" {
		t.Errorf("agent PromptFile = %q; want empty (instructions live in the agent)", agent.PromptFile)
	}

	wantErr := map[string]string{
		"nokind":          "kind is required",
		"badkind":         `kind "evaluations" unknown`,
		"agent-type":      "drop the type field",
		"agent-store":     "drop the store field",
		"agent-anthropic": `provider "anthropic" can't serve it`,
	}
	for name, want := range wantErr {
		if _, err := f.Resolve(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve(%s) error = %v; want it to contain %q", name, err, want)
		}
	}
}

// TestResolveStream pins the stream knob: kind-aware defaults (model
// streams, agent doesn't), a declared value beats the default in either
// direction, and a non-streaming request on an api without a buffered
// mode is rejected loudly.
func TestResolveStream(t *testing.T) {
	dir := t.TempDir()
	yaml := "profiles:\n" +
		"  model-default:\n    kind: model\n    api:\n      provider: openai\n      type: responses\n      base: https://x\n    auth:\n      credentials: env\n" +
		"  model-buffered:\n    kind: model\n    api:\n      provider: openai\n      type: responses\n      stream: false\n      base: https://x\n    auth:\n      credentials: env\n" +
		"  agent-default:\n    kind: agent\n    api:\n      base: https://x/agents/a\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n" +
		"  agent-streamed:\n    kind: agent\n    api:\n      stream: true\n      base: https://x/agents/a\n      headers:\n        api-key: fixed\n    auth:\n      credentials: env\n" +
		"  chat-buffered:\n    kind: model\n    api:\n      provider: openai\n      type: chat\n      stream: false\n      base: https://x\n    auth:\n      credentials: env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "k")
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"model-default":  true,
		"model-buffered": false,
		"agent-default":  false,
		"agent-streamed": true,
	}
	for name, stream := range want {
		cfg, err := f.Resolve(name)
		if err != nil {
			t.Errorf("Resolve(%s) error = %v", name, err)
			continue
		}
		if cfg.Stream != stream {
			t.Errorf("%s: Stream = %v; want %v", name, cfg.Stream, stream)
		}
	}
	if _, err := f.Resolve("chat-buffered"); err == nil || !strings.Contains(err.Error(), "stream: false needs api type responses") {
		t.Errorf("Resolve(chat-buffered) error = %v; want the stream/api mismatch named", err)
	}
}

// TestCredentialsFollowTheWorkingDirectory pins the rule for the one path
// it would be easiest to make an exception of. Every path in the config is
// resolved against the working directory — a profile's credentials file no
// differently, so there is one rule to know rather than one plus a caveat.
func TestCredentialsFollowTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	yaml := "profiles:\n" +
		"  dev:\n    kind: model\n    api:\n      provider: openai\n" +
		"      base: https://dev.example.net\n      model: m\n" +
		"    auth:\n      credentials: .env.dev\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// The credentials file sits where the app is *run*, not beside the
	// config that names it.
	run := t.TempDir()
	if err := os.WriteFile(filepath.Join(run, ".env.dev"),
		[]byte("OPENAI_API_KEY=from-the-working-directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(run)

	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := f.Resolve("dev")
	if err != nil {
		t.Fatalf("Resolve() error = %v; want the credentials found in the working directory", err)
	}
	if cfg.APIKey != "from-the-working-directory" {
		t.Errorf("APIKey = %q; want the key from the .env where the app was run", cfg.APIKey)
	}
}
