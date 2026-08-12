# Remote execution

Evolve can execute evaluation runs on a [patchy](https://oss.bitwisemedia.uk/patchy/) cluster instead of your
workstation: the run is planned locally, executed in sandboxed per-unit Kubernetes Jobs, and the results land in your
local `results.<ext>` files exactly as a local run would have written them. Local execution remains the default and
fully supported — remote is opt-in per run (`--remote`) or per repository (`remote.default`).

Why run remotely:

- **No harness CLIs or credentials on your machine.** The cluster's runner images carry the CLIs; the model API keys
  live in the cluster. Your machine needs only `evolve` and a sign-in.
- **Parallel capacity.** Units (skill × model × tier) fan out across the cluster's job scheduler instead of your
  laptop's cores.
- **Isolation.** Each unit runs in a locked-down pod: read-only rootfs, no Kubernetes credentials, egress limited to its
  own model API.

## Setup

Point evolve at the service and sign in once:

```sh
evolve login --remote-url https://patchy-evals.example.com
```

The service advertises its OIDC issuer and client id, so there is nothing else to configure: the login is an
authorization-code + PKCE flow through your browser (SSO), redirecting to an ephemeral localhost listener.
`--no-browser` prints the URL without launching anything. The credential is stored per remote URL in your user
configuration directory (`evolve/credentials.json`) — evolve's one user-level file; everything else stays repo-scoped —
and refreshes itself until the identity provider says otherwise, at which point `evolve login` again.

Pin the remote in `.evolve.yaml` to drop the flag:

```yaml
remote:
    url: https://patchy-evals.example.com
    # default: true   # run remotely unless --local
```

## Running

```sh
evolve run evals --remote --skill my-skill
evolve run triggers --remote
evolve run all --remote        # checks stay local; agent tiers run remotely
```

What happens:

1. The run is planned locally with the same engine predicates a local run uses — `--skill`, `--model`,
   `--new/--failed/--modified` all behave identically, computed against your local results files.
2. Each unit's workspace is bundled deterministically (skills + eval specs + fixtures; results files excluded),
   deduplicated by digest, and uploaded once.
3. The submission is monitored over a server-sent-event stream; progress replays onto the normal plain output. A dropped
   connection reconnects and reconciles — closing your laptop mid-run loses nothing, and rerunning the command later
   lands whatever finished. `Ctrl-C` cancels the run server-side.
4. Every finished unit's entry merges into `evals/<skill>/results.<ext>` with the same snapshot rotation as a local run;
   `evolve report` and `evolve view` render identically either way.

Remote runs are plain-output for now (the TUI's selection form probes local CLIs); `--count-only` stays local by
definition.

## Constraints

- The v1 runner fleet covers the claude, codex, and copilot harnesses; a model whose harnesses are all absent from the
  cluster's fleet is reported as skipped, never silently dropped.
- The LLM judge runs in-pod on the unit's own harness, so the judge model must be runnable by that harness.
- A workspace bundle unused for the server's retention window is swept; a run that needs it again reports
  `workspace expired server-side` — rerun to re-upload and resubmit.

## Access

Submitting requires RBAC on the service side: your SSO identity (as mapped by the cluster's claims configuration) needs
`create`/`get`/`delete` on the `evaluations` resource. Ask your operator for the `patchy-evaluations-submitter` tier.
