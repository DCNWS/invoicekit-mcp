# invoicekit-mcp

An [MCP](https://modelcontextprotocol.io) server for European e-invoices: it
validates EN 16931 documents, audits Factur-X and ZUGFeRD PDFs, and converts
between CII and UBL — **entirely inside your own process**. No invoice leaves
the machine, and no network call is made.

```
invoice.xml ──▶ invoicekit-mcp ──▶ valid: false
                                    BR-CO-10  (en16931)  Sum of invoice line net amounts doesn't match the declared total
```

An invoice carries a seller's IBAN and a buyer's address. A validator that
posts both to a remote API to check a rule set has made a decision the invoice's
owner never agreed to — and it is usually a third-party API with its own
retention policy, its own uptime, and its own idea of what "free tier" means.
Here the official CEN EN 16931 Schematrons, the KoSIT XRechnung CIUS and the
Peppol BIS bundle are compiled into the binary by the pure-Go engine
[`invoicekit.go`](https://github.com/DCNWS/invoicekit.go), and the answers come
from those bytes. That is the whole design brief: an agent should be able to
check a real invoice without a vendor finding out.

## Install

```
go install github.com/dcnws/invoicekit-mcp/cmd/invoicekit-mcp@latest
```

Or download a static binary for Linux, macOS or Windows from the
[latest release](https://github.com/DCNWS/invoicekit-mcp/releases) — one file,
no CGO, no runtime.

```
invoicekit-mcp -version   # the engine release and every rule set it carries
```

## Configure

Point any stdio MCP client at it. For Claude Desktop, `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "invoicekit": {
      "command": "/usr/local/bin/invoicekit-mcp"
    }
  }
}
```

Claude Code:

```
claude mcp add invoicekit -- /usr/local/bin/invoicekit-mcp
```

Codex (`~/.codex/config.toml`), Cursor, Continue, Zed and every other client
that speaks MCP take the same command.

## Versions

This server has no version of its own to track: it is a transport around
`invoicekit.go`, so a release of this repository is tagged with the engine
release it links, and `info` reports the same number. A tag of `v0.2.13` means
"engine 0.2.13, nothing else", and CI fails if the tag and `go.mod` ever
disagree. The rule-set versions inside that engine come from the publishers'
own stamps, printed by `invoicekit-mcp -version`.

## Tools

| tool | what it answers |
| --- | --- |
| `validate` | Is this invoice conformant, and which rules does it break? CII and UBL, EN 16931 plus an optional CIUS (`cius: "xrechnung"`) or network bundle (`profile: "peppol"`). |
| `audit` | Is this Factur-X / ZUGFeRD PDF a valid hybrid document — PDF/A-3 envelope, attachment, profile metadata — and is the invoice inside it conformant? |
| `extract` | Give me the invoice XML that is inside this PDF, with the declared profile and the syntax that was found. |
| `convert` | Rewrite this invoice from CII to UBL or back, listing every value the target syntax cannot carry instead of dropping it. |
| `rules` | What does `BR-CO-10` actually require? The requirement text and the test behind the id, filtered by fragment. |
| `info` | Which engine release is this, and which version of each rule set produced the verdict you are reading? |

`validate` and `audit` take either `path` (a file on this machine) or
`document_base64` (the bytes in the request), never both. Both return the two
verdicts apart: structural problems against the syntax's XML schema, and
business-rule findings, each with its rule id and the rule set it came from.

### What a finding looks like

```json
{
  "valid": false,
  "syntax": "CII",
  "cius": "xrechnung",
  "fatal": 1,
  "findings": [
    {
      "id": "BR-CO-10",
      "source": "en16931",
      "flag": "fatal",
      "message": "Sum of invoice line net amounts doesn't match the declared total",
      "context": "/rsm:CrossIndustryInvoice/..."
    }
  ],
  "schema": []
}
```

Quote the `id` and the `source` and a supplier can be told exactly which rule
their file breaks, and from which standard it comes.

## Rule sets

Read from the version stamps the publishers put into their own files, so a
verdict can name its artefacts:

| rule set | source |
| --- | --- |
| EN 16931 (CII and UBL) | [Connecting Europe Facility](https://github.com/ConnectingEurope/eInvoicing-EN16931), preprocessed Schematrons |
| XRechnung CIUS | [KoSIT](https://xeinkauf.de), the German CIUS for public administration |
| Peppol BIS Billing 3.0 | [Peppol](https://github.com/peppol/einvoicing), the network's own bundle |

Run `invoicekit-mcp -version` to see the exact versions in the binary you are
about to trust. All of them are embedded at build time; `info` reports them, and
`offline: true` is a fact rather than a setting.

## Conformance

The engine underneath this server is validated against the ETSI/CEF conformance
corpus and the sample documents that ship with the official rule sets, and every
finding it reports is a rule id from those published files — not a paraphrase,
and not a heuristic. Where the engine cannot know something it says so: a rule
that its XPath subset cannot evaluate is reported as an internal finding rather
than answered quietly.

## Licence

MIT (see `LICENSE`). The EN 16931, XRechnung and Peppol rule sets embedded in
the binary are redistributed unmodified under their own licences — see
`NOTICE.md`.
