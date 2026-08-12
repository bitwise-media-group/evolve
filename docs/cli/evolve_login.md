## evolve login

Sign in to a patchy remote-evaluation service.

### Synopsis

Sign in to a patchy remote-evaluation service.

The service advertises its OIDC issuer and client id (GET /api/v1/auth/info),
so the only configuration is the service URL itself: --remote-url, or the
remote.url config key (env EVOLVE_REMOTE_URL). The flow is authorization-code
with PKCE against a public client — no client secret anywhere — redirecting to
an ephemeral localhost listener; a browser opens best-effort and the URL is
always printed for the times it cannot.

The credential is stored per remote URL in the user configuration directory
(evolve/credentials.json), not in any repository.

```
evolve login [flags]
```

### Options

```
  -h, --help                help for login
      --no-browser          print the sign-in URL only; never launch a browser
      --remote-url string   patchy remote-evaluation service URL (default: the remote.url config key)
```

### Options inherited from parent commands

```
      --json                    emit machine-readable JSONL progress on stdout
      --layout string           repository layout: auto, marketplace, multi, or single (default "auto")
      --results-format string   format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)
      --root string             repository root to operate on (default: walk up from the current directory)
      --telemetry-dir string    write OpenTelemetry traces/metrics/logs as JSON to this directory (default: off; overrides OTEL_* env vars)
  -v, --verbose                 enable debug logging
```

### SEE ALSO

* [evolve](evolve.md)	 - Evaluate coding-agent plugins: static checks, trigger accuracy, behavioral evals, reports

