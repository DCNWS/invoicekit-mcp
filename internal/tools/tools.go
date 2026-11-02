// Package tools holds what the invoicekit-mcp server offers, separated from
// the transport that carries it. Every function here is an ordinary Go call:
// it takes a plain parameter struct, returns a plain result struct, and knows
// nothing about JSON-RPC. That is what lets the whole surface be tested
// without a client, and what keeps the server thin.
//
// The engine sits below: github.com/dcnws/invoicekit.go runs the official
// EN 16931 Schematrons for CII and UBL in pure Go, audits Factur-X and ZUGFeRD
// PDF envelopes, and converts between the two syntaxes. Nothing in this package
// makes a network call, and nothing needs one: the rule sets are compiled into
// the binary.
package tools

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dcnws/invoicekit.go"
	"github.com/dcnws/invoicekit.go/facturx"
	"github.com/dcnws/invoicekit.go/hybrid"
)

// maxDocumentBytes bounds what one call may carry. Invoices are tens of
// kilobytes and hybrid PDFs a few megabytes, so the cap is three orders of
// magnitude above the ordinary case: it exists to stop a mistyped path or a
// stray base64 blob of a disk image from being parsed.
const maxDocumentBytes = 64 << 20

const (
	defaultRuleLimit = 100
	maxRuleLimit     = 2000
)

// ValidateParams names the document to check and the rule sets to check it
// against.
type ValidateParams struct {
	DocumentBase64 string   `json:"document_base64,omitempty" jsonschema:"The invoice XML as base64. Give this or path, never both."`
	Path           string   `json:"path,omitempty" jsonschema:"Path to the invoice XML on this machine. Give this or document_base64, never both."`
	Syntax         string   `json:"syntax,omitempty" jsonschema:"Force the syntax: CII or UBL. Empty detects it from the document."`
	CIUS           string   `json:"cius,omitempty" jsonschema:"Add a national rule set on top of EN 16931, for example xrechnung. Empty checks the norm alone."`
	Profile        string   `json:"profile,omitempty" jsonschema:"Run a network's rule-set bundle instead of the default one, for example peppol."`
	Phases         []string `json:"phases,omitempty" jsonschema:"Restrict the Schematron phases to run. Empty runs every phase the rule set activates by default."`
}

// Validate checks an invoice against the EN 16931 business rules and the XML
// schema of its syntax, and reports every rule it breaks. A document can pass
// one half and fail the other, so the two verdicts travel apart: findings for
// the rules, schema for the structure. valid is true only when both are clean.
func Validate(p ValidateParams) (*einvoice.Result, error) {
	opts, err := ruleOptions(p.Syntax, p.CIUS, p.Profile, p.Phases)
	if err != nil {
		return nil, err
	}
	data, err := loadDocument(p.DocumentBase64, p.Path)
	if err != nil {
		return nil, err
	}
	return einvoice.Validate(data, opts)
}

// AuditParams is the document to audit plus the rules its payload should be
// held to.
type AuditParams struct {
	DocumentBase64 string   `json:"document_base64,omitempty" jsonschema:"The hybrid PDF as base64. Give this or path, never both."`
	Path           string   `json:"path,omitempty" jsonschema:"Path to the hybrid PDF on this machine. Give this or document_base64, never both."`
	CIUS           string   `json:"cius,omitempty" jsonschema:"Add a national rule set on top of EN 16931 when checking the XML inside the PDF."`
	Profile        string   `json:"profile,omitempty" jsonschema:"Run a network's rule-set bundle against the XML inside the PDF, for example peppol."`
	Phases         []string `json:"phases,omitempty" jsonschema:"Restrict the Schematron phases run against the XML inside the PDF."`
}

// Audit checks a Factur-X, ZUGFeRD or Order-X PDF the way a recipient's
// software does: the PDF envelope first — PDF/A-3 conformance, the attachment,
// the metadata that declares the profile — and then the invoice XML carried
// inside it against the business rules. rules is absent when the PDF carries no
// XML at all, which makes a plain PDF a passing envelope and nothing more.
func Audit(p AuditParams) (hybrid.Result, error) {
	opts, err := ruleOptions("", p.CIUS, p.Profile, p.Phases)
	if err != nil {
		return hybrid.Result{}, err
	}
	data, err := loadDocument(p.DocumentBase64, p.Path)
	if err != nil {
		return hybrid.Result{}, err
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		return hybrid.Result{}, errors.New("audit reads a hybrid PDF; these bytes are not a PDF — validate reads invoice XML directly")
	}
	return hybrid.Check(data, opts)
}

// ExtractParams names the hybrid PDF whose payload is wanted.
type ExtractParams struct {
	DocumentBase64 string `json:"document_base64,omitempty" jsonschema:"The hybrid PDF as base64. Give this or path, never both."`
	Path           string `json:"path,omitempty" jsonschema:"Path to the hybrid PDF on this machine. Give this or document_base64, never both."`
}

// ExtractOutput is the invoice XML that was inside the PDF.
type ExtractOutput struct {
	DocumentBase64 string `json:"document_base64"`
	Syntax         string `json:"syntax"`
	Profile        string `json:"profile"`
	Standard       string `json:"standard"`
	Filename       string `json:"filename"`
	Bytes          int    `json:"bytes"`
}

// Extract recovers the invoice XML from a hybrid PDF, so it can be read,
// converted or validated on its own.
func Extract(p ExtractParams) (ExtractOutput, error) {
	data, err := loadDocument(p.DocumentBase64, p.Path)
	if err != nil {
		return ExtractOutput{}, err
	}
	xml, profile, err := facturx.Extract(data)
	if err != nil {
		return ExtractOutput{}, err
	}
	out := ExtractOutput{
		DocumentBase64: base64.StdEncoding.EncodeToString(xml),
		Profile:        profile.String(),
		Standard:       profile.Version(),
		Filename:       profile.Filename(),
		Bytes:          len(xml),
	}
	if sx, err := einvoice.Detect(xml); err == nil {
		out.Syntax = string(sx)
	}
	return out, nil
}

// ConvertParams names the document and the syntax to write it in.
type ConvertParams struct {
	DocumentBase64 string `json:"document_base64,omitempty" jsonschema:"The invoice XML, or a hybrid PDF containing one, as base64. Give this or path, never both."`
	Path           string `json:"path,omitempty" jsonschema:"Path to the invoice XML or hybrid PDF on this machine. Give this or document_base64, never both."`
	To             string `json:"to" jsonschema:"The syntax to write: CII or UBL."`
}

// ConvertOutput is the rewritten document and what the rewrite cost.
type ConvertOutput struct {
	DocumentBase64 string   `json:"document_base64"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	Bytes          int      `json:"bytes"`
	Issues         []string `json:"issues,omitempty"`
}

// Convert rewrites an invoice from one syntax to the other — CII to UBL for a
// Peppol channel, UBL to CII for a German authority — by way of the norm's
// abstract model rather than a field-by-field XSLT map. issues lists every value
// that could not survive the trip, which is the honest answer: a UBL document
// can carry things CII has no field for, and inventing a replacement would be
// worse than reporting the loss.
func Convert(p ConvertParams) (ConvertOutput, error) {
	to, err := parseSyntax(p.To)
	if err != nil {
		return ConvertOutput{}, err
	}
	data, err := loadDocument(p.DocumentBase64, p.Path)
	if err != nil {
		return ConvertOutput{}, err
	}
	if strings.HasPrefix(string(data), "%PDF") {
		data, _, err = facturx.Extract(data)
		if err != nil {
			return ConvertOutput{}, err
		}
	}
	parsed, err := einvoice.Parse(data)
	if err != nil {
		return ConvertOutput{}, err
	}
	out, err := einvoice.Generate(to, parsed.Invoice)
	if err != nil {
		return ConvertOutput{}, err
	}
	return ConvertOutput{
		DocumentBase64: base64.StdEncoding.EncodeToString(out),
		From:           string(parsed.Syntax),
		To:             string(to),
		Bytes:          len(out),
		Issues:         parsed.Issues,
	}, nil
}

// RulesParams selects which rule descriptions to list.
type RulesParams struct {
	Syntax     string `json:"syntax" jsonschema:"The syntax whose rules to list: CII or UBL."`
	IDContains string `json:"id_contains,omitempty" jsonschema:"Keep only rules whose id contains this text, for example BR-CO or BR-DE."`
	CIUS       string `json:"cius,omitempty" jsonschema:"Include a national rule set's rules, for example xrechnung."`
	Profile    string `json:"profile,omitempty" jsonschema:"List a network bundle's rules instead of the default ones, for example peppol."`
	Limit      int    `json:"limit,omitempty" jsonschema:"How many rules to return. Defaults to 100, maximum 2000."`
}

// RulesOutput is one page of rules and how much of the list it covers.
type RulesOutput struct {
	Rules     []einvoice.Rule `json:"rules"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated"`
}

// Rules lists the assertions a validation run would apply, with the text of
// each one and the XPath it tests. Asking by id fragment answers the question
// an invoice's finding raises — what exactly does BR-CO-05 require? — without
// shipping someone the whole norm.
func Rules(p RulesParams) (RulesOutput, error) {
	sx, err := parseSyntax(p.Syntax)
	if err != nil {
		return RulesOutput{}, err
	}
	opts, err := ruleOptions(p.Syntax, p.CIUS, p.Profile, nil)
	if err != nil {
		return RulesOutput{}, err
	}
	all, err := einvoice.Rules(sx, opts)
	if err != nil {
		return RulesOutput{}, err
	}
	needle := strings.ToUpper(strings.TrimSpace(p.IDContains))
	kept := make([]einvoice.Rule, 0, len(all))
	for _, r := range all {
		if needle == "" || strings.Contains(strings.ToUpper(r.ID), needle) {
			kept = append(kept, r)
		}
	}
	limit := p.Limit
	switch {
	case limit <= 0:
		limit = defaultRuleLimit
	case limit > maxRuleLimit:
		limit = maxRuleLimit
	}
	out := RulesOutput{Total: len(kept), Truncated: len(kept) > limit}
	if out.Truncated {
		kept = kept[:limit]
	}
	out.Rules = kept
	return out, nil
}

// RuleSet names one embedded rule set and the upstream release it came from.
type RuleSet struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Updated string `json:"updated,omitempty"`
}

// InfoOutput describes this build.
type InfoOutput struct {
	EngineVersion string    `json:"engine_version"`
	Offline       bool      `json:"offline"`
	Syntaxes      []string  `json:"syntaxes"`
	RuleSets      []RuleSet `json:"rule_sets"`
}

// Info reports the engine release, the fact that nothing leaves the machine,
// and the version of every rule set a verdict is made of — read from the
// stamps the publishers put into their own files, not from what this build
// remembers about them.
func Info() InfoOutput {
	out := InfoOutput{
		EngineVersion: einvoice.Version,
		Offline:       true,
		Syntaxes:      []string{string(einvoice.SyntaxCII), string(einvoice.SyntaxUBL)},
	}
	// The Commission publishes the EN 16931 assertions once per syntax, and
	// normally publishes them together. They are reported as one entry when
	// they carry the same stamp, so the version a caller reads is never an
	// average of two different ones.
	var version, updated string
	var mixed []einvoice.Artefact
	for _, a := range einvoice.Artefacts() {
		if version == "" {
			version, updated = a.Version, a.Updated
			continue
		}
		if a.Version != version {
			mixed = append(mixed, a)
		}
	}
	if len(mixed) == 0 {
		out.RuleSets = append(out.RuleSets, RuleSet{Name: "en16931", Version: version, Updated: updated})
	} else {
		for _, a := range einvoice.Artefacts() {
			out.RuleSets = append(out.RuleSets, RuleSet{
				Name:    "en16931-" + string(a.Syntax),
				Version: a.Version,
				Updated: a.Updated,
			})
		}
	}
	for _, c := range einvoice.CiusNames() {
		out.RuleSets = append(out.RuleSets, RuleSet{Name: c, Version: einvoice.CiusVersion(einvoice.CIUS(c))})
	}
	for _, pr := range einvoice.ProfileNames() {
		out.RuleSets = append(out.RuleSets, RuleSet{Name: pr, Version: einvoice.ProfileVersion(einvoice.Profile(pr))})
	}
	return out
}

// parseSyntax reads a syntax name from a caller, case and spacing aside.
func parseSyntax(s string) (einvoice.Syntax, error) {
	switch sx := einvoice.Syntax(strings.ToUpper(strings.TrimSpace(s))); sx {
	case einvoice.SyntaxCII, einvoice.SyntaxUBL:
		return sx, nil
	default:
		return "", fmt.Errorf("syntax must be CII or UBL, got %q", s)
	}
}

// ruleOptions builds engine options and refuses a rule set this build does not
// carry before anything else happens: a misspelt name is the caller's mistake,
// and reporting it among an invoice's findings would send someone off to repair
// a document that was never the problem.
func ruleOptions(syntax, cius, profile string, phases []string) (einvoice.Options, error) {
	opts := einvoice.Options{
		CIUS:    einvoice.CIUS(strings.TrimSpace(cius)),
		Profile: einvoice.Profile(strings.TrimSpace(profile)),
		Phases:  phases,
	}
	if strings.TrimSpace(syntax) != "" {
		sx, err := parseSyntax(syntax)
		if err != nil {
			return opts, err
		}
		opts.Syntax = sx
	}
	if err := einvoice.CheckRuleSets(opts); err != nil {
		return opts, err
	}
	return opts, nil
}

// loadDocument reads the bytes of a document from exactly one of the two
// sources a caller may name.
func loadDocument(b64, path string) ([]byte, error) {
	hasB64, hasPath := b64 != "", path != ""
	if hasB64 == hasPath {
		return nil, errors.New("provide exactly one of document_base64 and path")
	}
	var data []byte
	var err error
	if hasPath {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read document: %w", err)
		}
	} else {
		data, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("document_base64 is not valid base64: %w", err)
		}
	}
	if len(data) > maxDocumentBytes {
		return nil, fmt.Errorf("document is %d bytes, over the %d byte limit", len(data), int64(maxDocumentBytes))
	}
	return data, nil
}
