# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/bitwise-media-group/evolve/security/advisories/new). Do not open public
issues for security reports.

## Threat model (summary)

evolve is a CLI you install from signed releases and run locally to evaluate coding-agent plugins. Its security surface
is the integrity of the distributed binary and the parsing of untrusted authored input. It defends against:

- **Tampered release artifacts** — every release ships `checksums.txt`, a GitHub SLSA build-provenance attestation over
  those checksums, a keyless Sigstore (cosign) bundle per binary signed with the release workflow's GitHub OIDC
  identity, and an SPDX SBOM per archive, so a downloaded binary can be verified as exactly what the release workflow
  built.
- **Untrusted spec and config parsing** — evolve reads authored config (`.evolve.<ext>`) and trigger/eval spec files in
  JSON, JSONC, YAML, or TOML; decoding rejects malformed input with an error instead of crashing, and fixture path
  references in eval files are constrained to the evals directory so they cannot escape it via traversal.

Out of scope: the coding agents, providers, and plugin repositories evolve evaluates — it invokes the tools and runs
against the repositories you point it at, so vetting that third-party code is the operator's responsibility. Also out of
scope are a compromise of the release workflow's signing identity (that identity is the trust anchor) and a compromise
of the machine running evolve.

## Code scanning triage

CodeQL findings are triaged in [`security/code-scanning/index.md`](security/code-scanning/index.md), with a report per
finding recording why it was dismissed or how it was remediated.
