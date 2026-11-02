# Notices and licences

## Code

The Go source of `github.com/dcnws/invoicekit-mcp` is licensed under the MIT
licence (see `LICENSE`).

## Rule sets inside the binary

This server links `github.com/dcnws/invoicekit.go`, which embeds the official
validation artefacts of EN 16931 (published by the European Union through the
Connecting Europe Facility, EUPL v1.2), the KoSIT XRechnung CIUS rule sets and
the Peppol BIS Billing 3.0 bundle, each redistributed unmodified under its own
terms. Compiled binaries of this server therefore contain those files. Keep them
unmodified and keep this notice if you redistribute them.

The full terms and the upstream sources are listed in the engine's `NOTICE.md`:
<https://github.com/DCNWS/invoicekit.go/blob/main/NOTICE.md>.
