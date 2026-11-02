package tools

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcnws/invoicekit.go"
)

// sample reads a document from the repository's testdata directory.
func sample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return b
}

func TestValidateBase64(t *testing.T) {
	doc := base64.StdEncoding.EncodeToString(sample(t, "cii-xrechnung.xml"))
	res, err := Validate(ValidateParams{DocumentBase64: doc, CIUS: "xrechnung"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Syntax != einvoice.SyntaxCII {
		t.Errorf("syntax = %q, want %q", res.Syntax, einvoice.SyntaxCII)
	}
	if res.Fatal != 0 {
		t.Errorf("fatal = %d, want 0: %+v", res.Fatal, res.Findings)
	}
	if res.CIUS != einvoice.CIUSXRechnung {
		t.Errorf("cius = %q, want %q", res.CIUS, einvoice.CIUSXRechnung)
	}
}

func TestValidatePath(t *testing.T) {
	res, err := Validate(ValidateParams{Path: filepath.Join("..", "..", "testdata", "ubl-peppol.xml"), Profile: "peppol"})
	if err != nil {
		t.Fatalf("validate by path: %v", err)
	}
	if res.Syntax != einvoice.SyntaxUBL {
		t.Errorf("syntax = %q, want %q", res.Syntax, einvoice.SyntaxUBL)
	}
}

func TestValidateSourceErrors(t *testing.T) {
	doc := base64.StdEncoding.EncodeToString([]byte("<r/>"))
	for _, tc := range []struct {
		name string
		in   ValidateParams
		want string
	}{
		{"both", ValidateParams{DocumentBase64: doc, Path: "x.xml"}, "one of"},
		{"neither", ValidateParams{}, "one of"},
		{"bad base64", ValidateParams{DocumentBase64: "not base64!"}, "base64"},
		{"missing file", ValidateParams{Path: "/nope/nope.xml"}, "nope.xml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Validate(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateUnknownRuleSetAndSyntax(t *testing.T) {
	doc := base64.StdEncoding.EncodeToString(sample(t, "cii-xrechnung.xml"))
	if _, err := Validate(ValidateParams{DocumentBase64: doc, CIUS: "germany"}); err == nil || !strings.Contains(err.Error(), "xrechnung") {
		t.Errorf("unknown CIUS: err = %v, want it to name xrechnung", err)
	}
	if _, err := Validate(ValidateParams{DocumentBase64: doc, Syntax: "pdf"}); err == nil || !strings.Contains(err.Error(), "CII") {
		t.Errorf("unknown syntax: err = %v, want it to list CII", err)
	}
}

func TestConvertRoundTrip(t *testing.T) {
	doc := base64.StdEncoding.EncodeToString(sample(t, "cii-xrechnung.xml"))
	out, err := Convert(ConvertParams{DocumentBase64: doc, To: "ubl"})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	back, err := base64.StdEncoding.DecodeString(out.DocumentBase64)
	if err != nil {
		t.Fatalf("output is not base64: %v", err)
	}
	if !bytes.Contains(back, []byte("Invoice")) {
		t.Errorf("converted document does not look like an invoice: %.200s", back)
	}
	res, err := Validate(ValidateParams{DocumentBase64: out.DocumentBase64})
	if err != nil {
		t.Fatalf("validate converted: %v", err)
	}
	if res.Syntax != einvoice.SyntaxUBL {
		t.Errorf("converted syntax = %q, want %q", res.Syntax, einvoice.SyntaxUBL)
	}
}

func TestExtractFromHybrid(t *testing.T) {
	doc := base64.StdEncoding.EncodeToString(sample(t, "hybrid-a3.pdf"))
	out, err := Extract(ExtractParams{DocumentBase64: doc})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	xml, err := base64.StdEncoding.DecodeString(out.DocumentBase64)
	if err != nil {
		t.Fatalf("output is not base64: %v", err)
	}
	if !bytes.Contains(xml, []byte("CrossIndustryInvoice")) {
		t.Errorf("payload is not a CII invoice: %.200s", xml)
	}
	if out.Profile == "" {
		t.Error("profile is empty, want the Factur-X level that was found")
	}
}

func TestAuditHybridAndReject(t *testing.T) {
	doc := base64.StdEncoding.EncodeToString(sample(t, "hybrid-a3.pdf"))
	res, err := Audit(AuditParams{DocumentBase64: doc})
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if res.Rules == nil {
		t.Fatal("no rules block: the hybrid document carries no invoice XML")
	}
	xmlDoc := base64.StdEncoding.EncodeToString(sample(t, "cii-xrechnung.xml"))
	if _, err := Audit(AuditParams{DocumentBase64: xmlDoc}); err == nil || !strings.Contains(err.Error(), "PDF") {
		t.Errorf("audit of XML: err = %v, want it to say the input is not a PDF", err)
	}
}

func TestRulesFilterAndLimit(t *testing.T) {
	res, err := Rules(RulesParams{Syntax: "cii", IDContains: "BR-CO", Limit: 5})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	if len(res.Rules) != 5 {
		t.Fatalf("got %d rules, want 5", len(res.Rules))
	}
	for _, r := range res.Rules {
		if !strings.Contains(r.ID, "BR-CO") {
			t.Errorf("rule %q does not match the filter", r.ID)
		}
	}
	if res.Total <= 5 {
		t.Errorf("total = %d, want more than the page", res.Total)
	}
	if !res.Truncated {
		t.Error("truncated = false, want true when the page is cut")
	}
}

func TestInfoNamesTheArtefacts(t *testing.T) {
	in := Info()
	if in.EngineVersion != einvoice.Version {
		t.Errorf("engine version = %q, want %q", in.EngineVersion, einvoice.Version)
	}
	if len(in.RuleSets) == 0 {
		t.Fatal("no rule sets reported")
	}
	var sawNorm bool
	for _, rs := range in.RuleSets {
		if rs.Version == "" {
			t.Errorf("rule set %q has no version", rs.Name)
		}
		if rs.Name == "en16931" {
			sawNorm = true
		}
	}
	if !sawNorm {
		t.Error("the EN 16931 rule set is not reported")
	}
	if !in.Offline {
		t.Error("offline = false, want the no-network claim stated")
	}
}
