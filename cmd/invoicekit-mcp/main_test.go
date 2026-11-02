package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcnws/invoicekit.go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// session connects a real client to the server over an in-memory transport,
// which exercises the wiring this file owns: the tool names, their schemas, and
// the trip a result takes through JSON.
func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server, client := mcp.NewInMemoryTransports()
	if _, err := newServer().Connect(context.Background(), server, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	return res
}

func TestToolList(t *testing.T) {
	tools, err := session(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := []string{"audit", "convert", "extract", "info", "rules", "validate"}
	var got []string
	for _, tool := range tools.Tools {
		got = append(got, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", got, want)
	}
}

func TestCallValidateOverTheWire(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cii-xrechnung.xml"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	res := call(t, session(t), "validate", map[string]any{
		"document_base64": base64.StdEncoding.EncodeToString(data),
		"cius":            "xrechnung",
	})
	if res.IsError {
		t.Fatalf("validate reported an error: %s", content(res))
	}
	var out einvoice.Result
	decode(t, res, &out)
	if out.Syntax != einvoice.SyntaxCII {
		t.Errorf("syntax = %q, want CII", out.Syntax)
	}
}

func TestCallInfoNamesTheEngine(t *testing.T) {
	res := call(t, session(t), "info", map[string]any{})
	if res.IsError {
		t.Fatalf("info reported an error: %s", content(res))
	}
	var out struct {
		EngineVersion string `json:"engine_version"`
		Offline       bool   `json:"offline"`
		RuleSets      []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"rule_sets"`
	}
	decode(t, res, &out)
	if out.EngineVersion != einvoice.Version {
		t.Errorf("engine_version = %q, want %q", out.EngineVersion, einvoice.Version)
	}
	if !out.Offline {
		t.Error("offline = false")
	}
	if len(out.RuleSets) < 2 {
		t.Fatalf("rule_sets = %v, want the norm and its customisations", out.RuleSets)
	}
}

// TestCallReportsBadInputAsAToolError checks the shape a client sees when a
// call is wrong: an MCP tool error with a message that says what to fix, not a
// transport failure and not a verdict on a document that was never read.
func TestCallReportsBadInputAsAToolError(t *testing.T) {
	cs := session(t)
	res := call(t, cs, "validate", map[string]any{"cius": "xrechnung"})
	if !res.IsError {
		t.Fatalf("missing document: IsError = false, want true (%s)", content(res))
	}
	if !strings.Contains(content(res), "one of") {
		t.Errorf("missing document: message %q does not say what to provide", content(res))
	}
}

// decode moves a call's structured content into a typed value the way a client
// would: through JSON, so a field name that drifts fails the test.
func decode(t *testing.T, res *mcp.CallToolResult, dst any) {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("no structured content in the result (%s)", content(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encode structured content: %v", err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode structured content %s: %v", b, err)
	}
}

// content flattens the text blocks of a result so assertions read plainly.
func content(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
