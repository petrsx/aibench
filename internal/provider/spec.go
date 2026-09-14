// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"strconv"
	"strings"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// Candidate maps one concrete response header to the label shown when it
// wins the switch.
type Candidate struct {
	Header string
	Label  string
}

// MetricDef is one presentable metric: an ordered switch over equivalent
// headers across the gateway and provider apis. The first candidate
// present decides both the label and the value, so a gateway-fronted backend
// and a direct provider both populate the row — each under its own name.
type MetricDef struct {
	Candidates []Candidate
}

// Metric builds the common case: one label, any of the given headers.
func Metric(label string, headers ...string) MetricDef {
	d := MetricDef{Candidates: make([]Candidate, 0, len(headers))}
	for _, h := range headers {
		d.Candidates = append(d.Candidates, Candidate{Header: h, Label: label})
	}
	return d
}

// Pick resolves the metric against captured response headers.
func (d MetricDef) Pick(headers []capture.KeyValue) (capture.KeyValue, bool) {
	for _, c := range d.Candidates {
		for _, kv := range headers {
			if strings.EqualFold(kv.Key, c.Header) && kv.Value != "" {
				return capture.KeyValue{Key: c.Label, Value: kv.Value}, true
			}
		}
	}
	return capture.KeyValue{}, false
}

// Resolve picks every metric that is present, keeping definition order.
func Resolve(defs []MetricDef, headers []capture.KeyValue) []capture.KeyValue {
	var out []capture.KeyValue
	for _, d := range defs {
		if kv, ok := d.Pick(headers); ok {
			out = append(out, kv)
		}
	}
	return out
}

// ParamDef is one adjustable request property in the api's wire shape:
// Name doubles as the key ParseParams switches on when it fills
// provider.Params, Hint carries the provider's range as the input placeholder.
type ParamDef struct {
	Name string
	Hint string
}

// ParseParams builds the transport Params from raw string values (the
// prompt file's frontmatter, the Prompt tab's inputs) keyed by the
// spec's param names: only the knobs this api accepts are read, and an
// empty or unparsable value is simply not sent. Shared by the TUI and
// the CLI — assembling a request must not need a view.
func (s Spec) ParseParams(values map[string]string) Params {
	var p Params
	for _, def := range s.Params {
		raw := strings.TrimSpace(values[def.Name])
		if raw == "" {
			continue
		}
		pf := func() *float32 {
			v, err := strconv.ParseFloat(raw, 32)
			if err != nil {
				return nil
			}
			f := float32(v)
			return &f
		}
		switch def.Name {
		case "temperature":
			p.Temperature = pf()
		case "top_p":
			p.TopP = pf()
		case "top_k":
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				p.TopK = n
			}
		case "max_tokens", "max_output_tokens": // the responses api's name for the cap
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				p.MaxTokens = n
			}
		case "frequency_penalty":
			p.FrequencyPenalty = pf()
		case "presence_penalty":
			p.PresencePenalty = pf()
		case "seed":
			if n, err := strconv.Atoi(raw); err == nil {
				p.Seed = &n
			}
		case "reasoning.effort", "reasoning_effort":
			// Spelled out rather than range-checked: the accepted set
			// grows with the models, and a value this build has not heard
			// of is the endpoint's to reject, with its own message.
			p.ReasoningEffort = raw
		case "text.verbosity", "verbosity":
			p.Verbosity = raw
		case "stop":
			for s := range strings.SplitSeq(raw, ",") {
				if s = strings.TrimSpace(s); s != "" {
					p.Stop = append(p.Stop, s)
				}
			}
		}
	}
	return p
}

// Spec describes how one provider's exchanges present in the UI. Adding a
// provider means defining its Spec next to its client and wiring both into
// SpecFor and NewClient.
type Spec struct {
	LogMeta    []MetricDef // exchange-log meta line
	RateLimits []MetricDef // Metrics panel: rate limits
	Params     []ParamDef  // Prompt tab: this api's adjustable request properties
	Body       BodyShape   // Inspector: how this api lays a request body out
}

// BodyShape is where an api keeps the things worth measuring in a request
// body — the conversation, and the top-level fields big enough to be the
// answer on their own. The three wires disagree on every name: chat sends
// "messages" with the system prompt among them, the responses api sends
// "input" alongside a top-level "instructions", and the messages api sends
// "messages" with a top-level "system". Those names are the api's own
// business, so they live here with the rest of it and the Inspector reads
// them off the spec.
type BodyShape struct {
	// Items is the key holding the conversation, and Unit is the word for
	// one entry of it: chat's are messages, and the responses api's input
	// carries tool calls and their outputs too, so they are items.
	Items string
	Unit  string
	// Parts are the other top-level keys worth a row of their own, in the
	// order they should be shown. A key the body does not carry is simply
	// not shown, so listing one costs nothing.
	Parts []string
	// LabelWidth is the widest label this api's own vocabulary produces —
	// its longest role, item type, or part name. A view sizes its label
	// column to this rather than to whatever one record happens to hold,
	// so the numbers beside it stay in the same column while the reader
	// walks from record to record. A record carrying something longer
	// still widens it; this is the floor, not a cap.
	LabelWidth int
}

// SpecFor picks the presentation spec for the api. The spec is
// host-agnostic: the Azure-only headers (e.g. retry-after-ms) ride as
// fallback candidates within each api's spec, so one spec serves the
// api whether it runs direct or on the Azure host.
func SpecFor(api string) Spec {
	switch api {
	case config.APIMessages:
		return anthropicSpec
	case config.APIResponses:
		return responsesSpec
	default:
		return openaiSpec
	}
}
