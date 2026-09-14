// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
)

// Explain condenses an SDK error into one human line for the record line
// and the chat: status + code + first sentence of the message, plus the
// content-filter categories when the backend reports them. The full error
// body stays visible in the raw capture.
func Explain(err error) string {
	var oai *openai.Error
	if errors.As(err, &oai) {
		// RawJSON only carries the body's "error" property; gateway
		// shapes without one (some gateways) survive in the response dump.
		raw := oai.RawJSON()
		if raw == "" {
			raw = responseBody(oai.DumpResponse(true))
		}
		return apiErrorLine(oai.StatusCode, oai.Code, oai.Message, raw)
	}
	var ant *anthropic.Error
	if errors.As(err, &ant) {
		raw := ant.RawJSON()
		if raw == "" {
			raw = responseBody(ant.DumpResponse(true))
		}
		return apiErrorLine(ant.StatusCode, string(ant.Type()), messageFrom(raw), raw)
	}
	return err.Error()
}

// responseBody strips the head off an httputil response dump.
func responseBody(dump []byte) string {
	if i := bytes.Index(dump, []byte("\r\n\r\n")); i >= 0 {
		return string(dump[i+4:])
	}
	return ""
}

func apiErrorLine(status int, code, message, raw string) string {
	// Gateways answer in shapes the SDK does not type (e.g. a
	// {"statusCode":403,"message":…}); dig the message out of the raw
	// body before falling back to a snippet of it.
	if message == "" {
		message = messageFrom(raw)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d", status)
	switch {
	case code != "":
		b.WriteString(" " + code)
	case http.StatusText(status) != "":
		b.WriteString(" " + http.StatusText(status))
	}
	if msg := firstSentence(message); msg != "" {
		b.WriteString(": " + msg)
	} else if snippet := rawSnippet(raw); snippet != "" {
		b.WriteString(": " + snippet)
	}
	if cf := contentFilterSummary(raw); cf != "" {
		b.WriteString(" — " + cf)
	}
	return b.String()
}

// rawSnippet keeps unparseable bodies visible without dumping them.
func rawSnippet(raw string) string {
	s := strings.Join(strings.Fields(raw), " ")
	if len(s) > 140 {
		s = s[:140] + "…"
	}
	return s
}

// firstSentence keeps error headlines to one line; API messages often
// continue with links and remediation prose.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

// messageFrom digs the message out of a raw error body, wrapped
// ({"error":{"message":…}}) or flat.
func messageFrom(raw string) string {
	var v struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return ""
	}
	if v.Message != "" {
		return v.Message
	}
	return v.Error.Message
}

// contentFilterSummary renders Azure OpenAI's content_filter_result into
// "hate: medium, jailbreak: detected" — only the categories that fired.
func contentFilterSummary(raw string) string {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return ""
	}
	result := findFilterResult(v)
	if result == nil {
		return ""
	}

	categories := make([]string, 0, len(result))
	for name := range result {
		categories = append(categories, name)
	}
	sort.Strings(categories)

	var parts []string
	for _, name := range categories {
		cat, ok := result[name].(map[string]any)
		if !ok {
			continue
		}
		filtered, _ := cat["filtered"].(bool)
		detected, _ := cat["detected"].(bool)
		severity, _ := cat["severity"].(string)
		switch {
		case filtered && severity != "":
			parts = append(parts, name+": "+severity)
		case filtered || detected:
			parts = append(parts, name+": detected")
		}
	}
	return strings.Join(parts, ", ")
}

// findFilterResult walks the decoded error body for the (Azure-nested)
// content_filter_result object.
func findFilterResult(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if r, ok := m["content_filter_result"].(map[string]any); ok {
		return r
	}
	for _, child := range m {
		if r := findFilterResult(child); r != nil {
			return r
		}
	}
	return nil
}
