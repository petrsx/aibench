// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Profile is one endpoint selection in aibench.yaml: a required `kind:`
// (what stands behind the endpoint — model or agent, always spelled out,
// never defaulted) over two groups and one reference — `api:` says what
// to speak and where (the wire shape, the route, and the verbatim
// headers/query the endpoint needs), `auth:` names the credentials
// source, and `prompt:` optionally pins a top-level prompt set by name
// (the set this profile starts on; without it the session keeps its
// current set — see prompts.go).
type Profile struct {
	Kind   string      `yaml:"kind"`
	API    ProfileAPI  `yaml:"api"`
	Auth   ProfileAuth `yaml:"auth"`
	Prompt string      `yaml:"prompt"`
}

// ProfileAPI is the profile's `api:` group. Provider (openai|anthropic)
// picks the SDK; Type picks which of that provider's APIs to speak — the
// wire shape (chat default; anthropic implies messages). The route is
// entirely Base's business — a template that may carry {model} — plus
// Query; there is no host knob.
type ProfileAPI struct {
	Provider string `yaml:"provider"`
	Type     string `yaml:"type"`
	// Store is the responses api's stateful mode (the official `store`
	// parameter): responses stored server-side, requests chained on
	// previous_response_id. Resolve rejects it on any other api type.
	Store bool `yaml:"store"`
	// Stream is the official `stream` parameter, declared per profile
	// with a kind-aware default (model: true; agent: false — the
	// hosted-agent endpoint answers stream:true with an empty event
	// stream, verified live 2026-07-19). A pointer so an explicit value
	// beats the default in either direction; responses api only.
	Stream *bool  `yaml:"stream"`
	Base   string `yaml:"base"`
	Model  string `yaml:"model"`
	// Headers are extra request headers sent verbatim (e.g. a gateway's
	// subscription key). Values expand ${VAR} from the credentials
	// source, so secrets stay in the env file, not the yaml.
	Headers map[string]string `yaml:"headers"`
	// Query are extra query parameters sent verbatim (e.g. api-version
	// on a Foundry agent endpoint). Same ${VAR} expansion as headers,
	// but values show unredacted in the capture — no secrets here.
	Query map[string]string `yaml:"query"`
}

// ProfileAuth is the profile's `auth:` group. Credentials stay out of the
// yaml: it only names its credentials source — an env file path, or "env"
// for the process environment alone.
type ProfileAuth struct {
	Credentials string `yaml:"credentials"`
}

// File is the app config: the profiles, and nothing the app ever writes —
// the active selection and preferences live in settings.json beside it
// (see settings.go), so this file stays strictly human-owned.
type File struct {
	Profiles map[string]Profile   `yaml:"profiles"`
	Prompts  map[string]PromptSet `yaml:"prompts"`
	// Tools names the tool files (see internal/bindings): key → path,
	// referenced by prompt sets — prompt-specific and shared tools alike
	// resolve through the same map (prompts.go).
	Tools map[string]string `yaml:"tools"`

	dir string // the config file's directory; relative paths resolve here
}

// explicitPath is the --config override; SetFilePath records it before
// anything resolves the config.
var explicitPath string

// SetFilePath pins the config location (the root command's --config
// flag). An explicit path is honored verbatim — pointing at a missing
// file is an error, never a fallback to the search.
func SetFilePath(p string) { explicitPath = p }

// FilePath is where the config lives. An explicit location wins outright
// (--config flag, then the CONFIG_FILE env the test scripts use), both
// verbatim even when missing. Otherwise viper searches the standard
// spots for aibench.yaml, first hit wins: the current dir, ./.aibench/
// (a repo keeping its config out of its root), then the user folder
// ~/.aibench/ (the ~/.aws / ~/.kube dotfolder pattern; %USERPROFILE% on
// Windows). Viper only *finds* this file — parsing stays on yaml.v3,
// which keeps header/query keys verbatim (viper lowercases keys). What
// the app itself writes lives in settings.json beside it (settings.go,
// plain encoding/json).
func FilePath() string {
	if explicitPath != "" {
		return explicitPath
	}
	if v := os.Getenv("CONFIG_FILE"); v != "" {
		return v
	}
	v := viper.New()
	v.SetConfigName("aibench")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath(".aibench")
	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(home, ".aibench"))
	}
	if err := v.ReadInConfig(); err == nil {
		return v.ConfigFileUsed()
	} else if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
		// The file exists but doesn't parse: hand its path to LoadFile so
		// the yaml error names the file, instead of a "not found".
		if used := v.ConfigFileUsed(); used != "" {
			return used
		}
	}
	return "aibench.yaml"
}

// LoadFile parses the config file; a missing file is not an error here
// (ok=false) — the launch path decides whether that is a first run to
// scaffold or a pinned path to report. Parsing is strict: an unknown or misspelled key is a named error, never a silently
// dropped knob (the fail-loud principle applied to the file itself).
func LoadFile(path string) (File, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return File{}, false, fmt.Errorf("%s: %w", path, err)
	}
	f.dir = filepath.Dir(path)
	return f, true, nil
}

// Resolve builds the effective Config for one profile: env-based
// defaults, the profile's credentials file consulted before the process
// env (never written into it — switching must not leak), and the
// profile's own fields overlaid last.
func (f File) Resolve(name string) (Config, error) {
	p, ok := f.Profiles[name]
	if !ok {
		return Config{}, fmt.Errorf("profile %q not in config", name)
	}

	lookup := osLookup
	var creds map[string]string // nil when the process env is the declared source
	if p.Auth.Credentials != "" && p.Auth.Credentials != "env" {
		var err error
		creds, err = godotenv.Read(p.Auth.Credentials)
		if err != nil {
			return Config{}, fmt.Errorf("profile %q credentials: %w", name, err)
		}
		lookup = func(key string) string {
			if v, ok := creds[key]; ok {
				return v
			}
			return os.Getenv(key)
		}
	}

	// The environment supplies credentials only; every endpoint field
	// comes from the profile.
	cfg, err := buildCredentials(lookup)
	if err != nil {
		return Config{}, fmt.Errorf("profile %q credentials: %w", name, err)
	}
	// The kind is always declared, never defaulted: a profile reads as
	// what it is. Each kind resolves its own wire invariants.
	switch strings.ToLower(strings.TrimSpace(p.Kind)) {
	case KindModel:
		cfg.Kind = KindModel
		cfg.API, err = normalize(p.API.Provider, p.API.Type)
		if err != nil {
			return Config{}, fmt.Errorf("profile %q: %w", name, err)
		}
		cfg.Store = p.API.Store
		cfg.Stream = true
	case KindAgent:
		if err := p.agentInvariants(); err != nil {
			return Config{}, fmt.Errorf("profile %q: %w", name, err)
		}
		cfg.Kind = KindAgent
		cfg.API = APIResponses
		cfg.Store = true
		cfg.Stream = false  // the hosted-agent endpoint streams nothing
		cfg.PromptFile = "" // instructions live in the agent, no client prompt
	case "":
		return Config{}, fmt.Errorf("profile %q: kind is required (model or agent)", name)
	default:
		return Config{}, fmt.Errorf("profile %q: kind %q unknown (want model or agent)", name, p.Kind)
	}
	// The declared stream value beats the kind default in either
	// direction — deliberately flipping an agent to stream: true (to
	// observe the endpoint's behavior) is this tool's kind of test.
	if p.API.Stream != nil {
		cfg.Stream = *p.API.Stream
	}
	if cfg.API == APIMessages {
		// ANTHROPIC_API_KEY, or the AZURE_API_KEY a Foundry deployment issues.
		env := func(key, fallback string) string {
			if v := lookup(key); v != "" {
				return v
			}
			return fallback
		}
		cfg.APIKey = anthropicKey(env, cfg.APIKey)
	}
	if p.API.Base != "" {
		cfg.APIBase = p.API.Base
	}
	if p.API.Model != "" {
		cfg.Model = p.API.Model
	}
	if len(p.API.Headers) > 0 {
		cfg.Headers = make(map[string]string, len(p.API.Headers))
		for k, v := range p.API.Headers {
			// ${VAR} / $VAR expand from the credentials source, so the
			// secret lives in the env file, not the yaml.
			cfg.Headers[k] = os.Expand(v, lookup)
		}
		// A profile that declares its auth headers doesn't inherit
		// ambient token auth from the shell: only its own credentials
		// file opts in. Without the guard, a service principal exported
		// for unrelated Azure work would silently ride a stealth Bearer
		// token alongside the declared header — muddying the diagnostics
		// this tool exists to capture. The app never assembles a
		// credential the profile didn't point at.
		if creds != nil && creds["AZURE_CLIENT_SECRET"] == "" {
			cfg.TenantID, cfg.ClientID, cfg.ClientSecret = "", "", ""
		}
		if creds != nil && creds["AZURE_TOKEN"] == "" {
			cfg.Token = ""
		}
		if creds != nil && creds["AZURE_USE_LOGIN"] == "" {
			cfg.UseLogin = false
		}
	}
	if len(p.API.Query) > 0 {
		cfg.Query = make(map[string]string, len(p.API.Query))
		for k, v := range p.API.Query {
			cfg.Query[k] = os.Expand(v, lookup)
		}
	}
	if err := cfg.validate(); err != nil {
		if errors.Is(err, ErrNoCredentials) {
			source := "the process environment"
			var defined []string
			if creds != nil {
				source = p.Auth.Credentials
				for k := range creds {
					defined = append(defined, k)
				}
			}
			return cfg, fmt.Errorf("profile %q: %s", name,
				CredentialHelp(source, defined, len(p.API.Headers) > 0))
		}
		return cfg, fmt.Errorf("profile %q: %w", name, err)
	}
	return cfg, nil
}

// agentInvariants rejects the knobs an agent endpoint implies or owns
// server-side — declaring them anyway is a contradiction, and invalid
// combinations always fail loudly rather than drop silently.
func (p Profile) agentInvariants() error {
	if prov := strings.ToLower(strings.TrimSpace(p.API.Provider)); prov != "" && prov != ProviderOpenAI {
		return fmt.Errorf("kind agent speaks the openai responses api (provider %q can't serve it)", p.API.Provider)
	}
	if p.API.Type != "" {
		return errors.New("kind agent always speaks the responses api — drop the type field")
	}
	if p.API.Store {
		return errors.New("kind agent implies server-side state — drop the store field")
	}
	return nil
}
