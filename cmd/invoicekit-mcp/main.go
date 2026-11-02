// Command invoicekit-mcp serves EN 16931 e-invoice validation over the Model
// Context Protocol on stdio, with the engine linked in rather than called over
// a network.
//
// The point of that arrangement is that an invoice never leaves the machine it
// was drawn on. An invoice carries a seller's bank details and a buyer's
// address, and a validation service that ships both away to check a rule set
// has made a decision the invoice's owner never agreed to. Here the official
// CEN EN 16931 Schematrons, the XRechnung CIUS and the Peppol BIS bundle are
// compiled into the binary, so the process answers from its own bytes: no API
// key, no outbound connection, no log on somebody else's server.
//
// Configure it as an MCP server (Claude Desktop, Codex, Claude Code, any stdio
// MCP client):
//
//	{
//	  "mcpServers": {
//	    "invoicekit": { "command": "/path/to/invoicekit-mcp" }
//	  }
//	}
//
// Or build it from the module: go install
// github.com/dcnws/invoicekit-mcp/cmd/invoicekit-mcp@latest
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dcnws/invoicekit-mcp/internal/tools"
	"github.com/dcnws/invoicekit.go"
	"github.com/dcnws/invoicekit.go/hybrid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	showVersion := flag.Bool("version", false, "print the engine release and the rule sets, then exit")
	flag.Parse()

	if *showVersion {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tools.Info()); err != nil {
			fmt.Fprintln(os.Stderr, "invoicekit-mcp:", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newServer().Run(ctx, &mcp.StdioTransport{}); err != nil {
		// stdout is the protocol channel, so a complaint has to go elsewhere.
		fmt.Fprintln(os.Stderr, "invoicekit-mcp:", err)
		os.Exit(1)
	}
}

// newServer builds the server and every tool it offers. It is a function
// rather than a block in main so the surface can be driven in tests over an
// in-memory transport, exactly as a client drives it in production.
func newServer() *mcp.Server {
	info := tools.Info()
	s := mcp.NewServer(&mcp.Implementation{
		Name:        "invoicekit",
		Title:       "InvoiceKit",
		Version:     info.EngineVersion,
		Description: "EN 16931 e-invoice validation, Factur-X and ZUGFeRD auditing and CII↔UBL conversion, running entirely on this machine.",
		WebsiteURL:  "https://invoicekit.dcnws.com",
	}, &mcp.ServerOptions{Instructions: instructions(info)})
	register(s)
	return s
}

// instructions is the short brief a client puts in front of the model before
// it picks a tool. It names the two things a model is otherwise obliged to
// guess: which tool answers which question, and which release the verdict
// came from.
func instructions(info tools.InfoOutput) string {
	sets := make([]string, 0, len(info.RuleSets))
	for _, rs := range info.RuleSets {
		sets = append(sets, rs.Name+" "+rs.Version)
	}
	return "InvoiceKit validates electronic invoices against the European norm EN 16931. " +
		"validate answers whether an invoice XML is conformant and names every rule it breaks; " +
		"audit checks a Factur-X or ZUGFeRD PDF envelope and the invoice inside it; " +
		"extract recovers the XML from a hybrid PDF; convert rewrites an invoice between CII and UBL; " +
		"rules explains what a rule id such as BR-CO-05 actually requires; " +
		"info reports the rule sets behind a verdict. " +
		"Everything runs in this process: no invoice leaves the machine, and no network call is made. " +
		"Rule sets this build carries: " + strings.Join(sets, ", ") + ". " +
		"Findings carry a rule id and a source, so quote both when explaining a problem."
}

// register adds the six tools. Each handler is one call into internal/tools:
// the logic lives there, tested without a protocol, and this file is only the
// translation between typed Go functions and MCP tool calls.
func register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "validate",
		Description: "Validate an EN 16931 invoice (CII or UBL XML) against the official business rules and its XML schema. " +
			"Returns valid, plus findings (rule id, source, message) and schema problems. Pass cius=xrechnung for German public-sector rules or profile=peppol for Peppol BIS.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, p tools.ValidateParams) (*mcp.CallToolResult, *einvoice.Result, error) {
		res, err := tools.Validate(p)
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "audit",
		Description: "Audit a hybrid invoice PDF — Factur-X, ZUGFeRD or Order-X — the way recipient software does: PDF/A-3 envelope, the attachment and the profile metadata, then the invoice XML inside it against the rules.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, p tools.AuditParams) (*mcp.CallToolResult, *hybrid.Result, error) {
		res, err := tools.Audit(p)
		return nil, &res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "extract",
		Description: "Recover the invoice XML embedded in a Factur-X or ZUGFeRD PDF, with the profile the document declares and the syntax that was found.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, p tools.ExtractParams) (*mcp.CallToolResult, tools.ExtractOutput, error) {
		out, err := tools.Extract(p)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "convert",
		Description: "Convert an invoice between CII and UBL through the EN 16931 model. Accepts invoice XML or a hybrid PDF. Any value the target syntax cannot carry is listed in issues rather than dropped silently.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, p tools.ConvertParams) (*mcp.CallToolResult, tools.ConvertOutput, error) {
		out, err := tools.Convert(p)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "rules",
		Description: "List the rules a validation run applies, with the requirement text and the test behind each id. Filter by id fragment (BR-CO, BR-DE, BR-X) to answer a finding from a previous call.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, p tools.RulesParams) (*mcp.CallToolResult, tools.RulesOutput, error) {
		out, err := tools.Rules(p)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "info",
		Description: "Report the engine release, the rule sets embedded in this build with their upstream versions, and confirmation that no network is used. Quote this when stating which artefacts a verdict came from.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, tools.InfoOutput, error) {
		return nil, tools.Info(), nil
	})
}
