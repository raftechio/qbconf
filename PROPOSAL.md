# qbconf v2 — Architecture Proposal

**Status:** Draft for discussion
**Scope:** Restructure qbconf from a single-file CLI into a modular, multi-cloud, fully testable Go project.

This proposal builds on the best-practices review of the current codebase. It covers: a new package layout with `main.go` under `cmd/`, separated cloud-provider packages behind interfaces, migration from `urfave/cli` v2 to `spf13/cobra` + `spf13/viper`, migration from `zap` to `zerolog`, a testing strategy, and targeted code optimizations.

---

## 1. Goals

1. **Extensibility** — adding a new cloud (GKE, AKS) or a new OIDC source (Bitbucket, CircleCI) must not touch existing code, only add a package.
2. **Testability** — every unit of logic testable without AWS credentials or network access; today the project has zero tests.
3. **Robustness** — no `panic`/`os.Exit` outside `main`, context propagation everywhere, correct retry semantics, no unchecked pointer dereferences.
4. **Modern stack** — cobra + viper for CLI/config, zerolog for logging, Go ≥ 1.24, no aws-sdk-go v1.
5. **Small footprint** — keep (and ideally shrink) the ~4 MB binary that makes qbconf attractive for CI images.

Non-goals: changing the CLI's observable behavior for existing users beyond documented flag changes; building a long-running service.

---

## 2. Proposed repository layout

```
qbconf/
├── cmd/
│   └── qbconf/
│       └── main.go                  # entrypoint only: build info, wire deps, os.Exit
├── internal/
│   ├── cli/                         # cobra command tree (no business logic)
│   │   ├── root.go                  # root cmd, global flags, viper binding, logger init
│   │   ├── generate.go              # `qbconf generate`
│   │   └── generate_aws.go          # `qbconf generate aws`
│   ├── config/
│   │   ├── config.go                # Config struct, Load() via viper, Validate()
│   │   └── config_test.go
│   ├── logging/
│   │   └── logging.go               # zerolog constructor (level, format, TTY detection)
│   ├── kubeconfig/                  # cloud-agnostic kubeconfig model + serialization
│   │   ├── kubeconfig.go            # Build(ClusterInfo, Token) -> []byte (YAML)
│   │   ├── writer.go                # atomic write, 0600 perms
│   │   ├── kubeconfig_test.go       # golden-file tests
│   │   └── testdata/
│   │       └── eks-basic.golden.yaml
│   ├── provider/
│   │   ├── provider.go              # Provider interface + ClusterInfo + registry
│   │   ├── registry_test.go
│   │   └── aws/
│   │       ├── provider.go          # implements provider.Provider for EKS
│   │       ├── provider_test.go
│   │       ├── clients.go           # narrow interfaces over SDK clients (mockable)
│   │       ├── credentials.go       # CredentialStrategy chain (default/assume-role/OIDC)
│   │       ├── credentials_test.go
│   │       ├── presign.go           # STS presigned GetCallerIdentity -> bearer token
│   │       ├── presign_test.go
│   │       └── oidc/
│   │           ├── oidc.go          # TokenSource interface
│   │           ├── github.go        # GitHub Actions implementation
│   │           ├── github_test.go   # httptest-based
│   │           ├── gitlab.go        # GitLab CI implementation
│   │           └── gitlab_test.go
│   └── retry/
│       ├── retry.go                 # generic, context-aware backoff with jitter
│       └── retry_test.go
├── .github/workflows/
│   ├── ci.yaml                      # lint + vet + test on PR
│   └── release.yaml                 # goreleaser on tag
├── .golangci.yml
├── .goreleaser.yaml                 # ldflags -> cmd/qbconf, main.version
├── go.mod                           # module github.com/raftechio/qbconf, go 1.24
└── README.md
```

Rationale:

- **`cmd/qbconf/main.go`** follows the standard Go project layout. `main` stays under ~30 lines: construct logger, load config, call `cli.Execute(ctx)`, map the returned error to an exit code. It is the *only* place allowed to call `os.Exit`.
- **`internal/`** prevents external modules from importing implementation details, which frees us to refactor without semver obligations. Nothing in this project is meant to be consumed as a library today; if that changes we can promote stable packages to `pkg/` deliberately.
- **One package per cloud** under `internal/provider/`, each depending only on the `provider` contract — never on each other.

---

## 3. Core abstractions

### 3.1 The provider contract

```go
// internal/provider/provider.go
package provider

import "context"

// ClusterInfo is the cloud-agnostic description of a cluster,
// sufficient to build a kubeconfig.
type ClusterInfo struct {
    Name     string
    Endpoint string
    CAData   []byte // decoded PEM, not base64
}

// Provider produces the two artifacts a kubeconfig needs:
// cluster metadata and a bearer token.
type Provider interface {
    // Name returns the provider key used on the CLI, e.g. "aws".
    Name() string
    // GetCluster resolves cluster connection metadata.
    GetCluster(ctx context.Context, clusterName string) (*ClusterInfo, error)
    // Token produces a short-lived bearer token for the cluster.
    Token(ctx context.Context, clusterName string) (string, error)
}
```

A tiny registry maps `"aws"` → constructor so `generate <cloud>` dispatches without a switch statement growing in the CLI layer:

```go
type Factory func(ctx context.Context, cfg *config.Config, log zerolog.Logger) (Provider, error)

func Register(name string, f Factory)          // called from each provider's init or explicit wiring
func New(ctx context.Context, name string, ...) (Provider, error)
```

Prefer **explicit registration in `cmd/qbconf/main.go`** over `init()` side effects — it keeps the dependency graph visible and avoids blank imports.

### 3.2 AWS credential strategies

Today three booleans (`--with-assume-role`, `--with-gha-oidc`, `--with-gitlab-oidc`) mutate a global `aws.Config`. Replace with a strategy interface selected once, validated up front:

```go
// internal/provider/aws/credentials.go
type CredentialStrategy interface {
    // Resolve returns a credentials provider layered on the base config.
    Resolve(ctx context.Context, base aws.Config) (aws.CredentialsProvider, error)
}

type DefaultChain struct{}                       // SDK default resolution
type AssumeRole struct{ RoleARN, SessionName string }
type WebIdentity struct {                        // OIDC-based
    RoleARN, SessionName string
    Source oidc.TokenSource
}
```

`WebIdentity.Resolve` should return the SDK's own `stscreds.NewWebIdentityRoleProvider` wrapped in `aws.NewCredentialsCache` — not static credentials captured once (the current code freezes credentials at fetch time; the SDK provider refreshes and retries natively).

### 3.3 OIDC token sources

```go
// internal/provider/aws/oidc/oidc.go
type TokenSource interface {
    // Token fetches a web-identity JWT for the given audience.
    Token(ctx context.Context, audience string) (string, error)
}
```

- `github.go`: reads `ACTIONS_ID_TOKEN_REQUEST_URL/TOKEN`, calls the endpoint with a plain `net/http` client (resty is not needed for one GET — one dependency removed), checks the HTTP status, parses JSON with `encoding/json` into a typed struct (gjson removed), and builds the URL with `net/url` instead of `fmt.Sprintf("%s&audience=…")`.
- `gitlab.go`: reads `CI_JOB_JWT_V2` (and the newer `ID_TOKEN` variable — GitLab has deprecated `CI_JOB_JWT_V2`); returns typed errors, never calls `os.Exit`.

### 3.4 Narrow SDK client interfaces (the key to testability)

Never mock the AWS SDK structs; define the minimal surface qbconf consumes:

```go
// internal/provider/aws/clients.go
type EKSDescriber interface {
    DescribeCluster(ctx context.Context, in *eks.DescribeClusterInput, opts ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

type STSPresigner interface {
    PresignGetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput, opts ...func(*sts.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type IdentityGetter interface {
    GetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput, opts ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}
```

The real SDK clients satisfy these implicitly; tests supply 5-line fakes. The `aws.Provider` struct receives them via its constructor:

```go
type Provider struct {
    eks      EKSDescriber
    presign  STSPresigner
    identity IdentityGetter
    log      zerolog.Logger
}
```

No package-level variables anywhere — every dependency is a field.

---

## 4. CLI framework: urfave/cli v2 → cobra + viper

### Why cobra

- First-class pairing with viper (both spf13), giving flag ⟷ env ⟷ config-file ⟷ default precedence for free.
- Idiomatic command tree in separate files, shell completions, and `--help` UX that scales with more providers.

### Command tree

```
qbconf
├── generate
│   └── aws        --cluster-name --region --role-arn --role-session-name
│                  --auth (default|assume-role|gha-oidc|gitlab-oidc)
│                  --output-file
├── version
└── completion
```

**Flag change (documented, with deprecation aliases):** the three mutually-unvalidated booleans collapse into one enum flag `--auth`, making invalid combinations unrepresentable. Keep the old booleans for one minor release as hidden deprecated aliases that map onto `--auth` and print a warning.

### Configuration with viper

```go
// internal/config/config.go
type Config struct {
    Region          string `mapstructure:"region"`
    ClusterName     string `mapstructure:"cluster_name"`
    RoleARN         string `mapstructure:"role_arn"`
    RoleSessionName string `mapstructure:"role_session_name"`
    Auth            string `mapstructure:"auth"`        // default|assume-role|gha-oidc|gitlab-oidc
    OutputFile      string `mapstructure:"output_file"`
    LogLevel        string `mapstructure:"log_level"`
    LogFormat       string `mapstructure:"log_format"`  // auto|json|console
    Timeout         time.Duration `mapstructure:"timeout"`
}

func Load(cmd *cobra.Command) (*Config, error)  // binds flags, QBCONF_* env, optional file
func (c *Config) Validate() error               // e.g. auth!=default requires role_arn
```

Precedence (viper default): **flags > env (`QBCONF_REGION`, …) > config file (`$PWD/.qbconf.yaml`, `$HOME/.config/qbconf/config.yaml`) > defaults.** Keep honoring `AWS_REGION`/`AWS_ROLE_ARN`/`AWS_ROLE_SESSION_NAME` via explicit `BindEnv` calls for backward compatibility.

`Validate()` runs in `PreRunE` so misconfiguration fails fast with an actionable message instead of an opaque STS error mid-flight.

---

## 5. Logging: zap → zerolog

`internal/logging/logging.go` exposes a single constructor:

```go
func New(level, format string, w io.Writer) zerolog.Logger
```

- `format=auto`: `zerolog.ConsoleWriter` when `w` is a TTY, JSON otherwise (CI logs stay machine-parseable, humans get readable output).
- Default level **warn** for a quiet-on-success CLI; `-v/--log-level debug` restores today's narration. The current step-by-step `Info` logs become `Debug`.
- The logger is passed down through constructors (`zerolog.Logger` is a value type, cheap to copy with added context: `log.With().Str("provider", "aws").Logger()`). The `request_uuid` field moves to root-level `With()` context.
- Rule: **log or return, never both.** Errors are wrapped with `%w` and context at each layer; only `main` logs the final error before exiting non-zero.

zerolog is also a footprint win: zero-allocation, no reflection, smaller than zap in the binary.

---

## 6. Robustness fixes folded into the refactor

These come straight from the review and are absorbed into the new structure rather than patched in place:

| Issue | Resolution in v2 |
|---|---|
| `os.Exit(1)` inside token fetch | Only `cmd/qbconf/main.go` exits; everything else returns errors |
| `writeToFile` panics, 0644 perms | `kubeconfig.Writer`: write to temp file in target dir, `chmod 0600`, `os.Rename` (atomic), propagate errors |
| Ignored base64/CA decode error | `GetCluster` validates and decodes CA, returns wrapped error |
| Nil SDK pointer derefs | `aws.ToString`/explicit nil checks in one place (`GetCluster`), typed error if cluster not ACTIVE |
| Dead aws-sdk-go **v1** error handling | v1 dependency deleted; error inspection via `smithy.APIError` where useful |
| Retry sleeps after last attempt, no jitter/ctx | `internal/retry`: generic `Do(ctx, policy, fn)` — respects `ctx.Done()`, full jitter, no trailing sleep; unit-tested with a fake clock |
| Unchecked OIDC HTTP status / silent empty token | status checked, token validated non-empty, JSON typed |
| `context.TODO()` everywhere | `main` creates `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` + `--timeout` (default 2m); ctx threaded through every call |
| Global `awsConfig`/logger state | all dependencies constructor-injected |
| `io/ioutil`, go 1.19, module path `RaftechNL` | `os.WriteFile`, `go 1.24`, module `github.com/raftechio/qbconf` (v2 is the moment to fix the path) |

### Optional footprint optimization (recommended, measurable)

`k8s.io/client-go` is imported solely for `clientcmd.Write` and the `api.Config` structs — it is by far the heaviest dependency in the tree. Replacing it with a ~60-line local kubeconfig struct marshaled via `sigs.k8s.io/yaml` (or `gopkg.in/yaml.v3`) typically cuts the binary by 30–40 % and removes dozens of transitive modules. The golden-file tests (§7) guarantee byte-for-byte compatible output before/after the swap. Also drop the committed `vendor/` tree (1,800+ files) and rely on the module proxy; goreleaser builds remain reproducible via `go.sum`.

---

## 7. Testing strategy

Target: **≥ 80 % coverage on `internal/`**, enforced informally via CI report (not a hard gate initially).

1. **Unit tests (table-driven, no network):**
   - `retry`: attempts, jitter bounds, context cancellation mid-backoff — using an injected `sleep func(ctx, d)` so tests run in microseconds.
   - `config`: precedence (flag vs env vs file), `Validate()` matrix (auth mode × missing role-arn, …).
   - `provider/aws/credentials`: strategy selection, error paths, fake STS.
   - `provider/aws`: `GetCluster` against a fake `EKSDescriber` (nil endpoint, not-ACTIVE cluster, bad CA base64); `Token` against a fake `STSPresigner` asserting the `x-k8s-aws-id` header and `k8s-aws-v1.` prefix.
2. **OIDC tests with `httptest.Server`:** GitHub source — happy path, 403, malformed JSON, empty token, missing env vars (`t.Setenv`).
3. **Golden-file tests:** `kubeconfig.Build` output compared to `testdata/*.golden.yaml` with an `-update` flag; guards the client-go removal and any future serialization change.
4. **CLI smoke tests:** cobra commands executed with `cmd.SetArgs(...)`/`ExecuteContext` and a fake provider registered — asserts wiring, flag deprecation warnings, exit codes.
5. **Integration tests (opt-in):** `//go:build integration`, run against a real EKS cluster in a nightly workflow, skipped on PRs.

### CI (`.github/workflows/ci.yaml`)

```yaml
on: [pull_request, push]
jobs:
  ci:
    steps:
      - go build ./...
      - golangci-lint run          # errcheck, staticcheck, revive, gosec, unparam, ...
      - go test -race -cover ./...
      - goreleaser check
```

`gosec` in the linter set would have caught the 0644 kubeconfig permission on day one.

---

## 8. Migration plan

Phased so `main` stays releasable at every step:

- **Phase 0 — Safety net (small PR):** add CI workflow + golangci-lint on the *current* code; fix the four outright bugs (os.Exit, panic-in-writeToFile, 0600 perms, OIDC status check). Tag `v1.x` patch release.
- **Phase 1 — Skeleton:** create `cmd/qbconf/`, `internal/` layout; move code verbatim into packages; update `.goreleaser.yaml` (`builds[].main: ./cmd/qbconf`) and module path. Behavior unchanged.
- **Phase 2 — Contracts:** introduce `provider.Provider`, `CredentialStrategy`, `TokenSource`, narrow SDK interfaces; delete globals; thread `context.Context`. Land unit tests alongside each package (the tests are the acceptance criteria for this phase).
- **Phase 3 — Stack swap:** cobra + viper (with deprecated boolean aliases), zerolog, `internal/retry`. Golden-file tests lock output format first.
- **Phase 4 — Optimization & release:** drop aws-sdk-go v1, resty, gjson; optionally drop client-go; drop `vendor/`; bump `go 1.24`; release **v2.0.0** with a migration section in the README.

Each phase is one reviewable PR; Phases 2–3 are where the interface decisions in this document get exercised, so review effort should concentrate there.

---

## 9. Dependency delta

| | Before | After |
|---|---|---|
| CLI | urfave/cli v2 | spf13/cobra |
| Config | env vars via cli flags only | spf13/viper (flags/env/file) |
| Logging | uber-go/zap | rs/zerolog |
| HTTP | go-resty/resty | net/http (stdlib) |
| JSON | tidwall/gjson | encoding/json (stdlib) |
| AWS | aws-sdk-go **v1 + v2** | aws-sdk-go-v2 only |
| Kubeconfig | k8s.io/client-go | local struct + sigs.k8s.io/yaml *(optional, recommended)* |
| Go | 1.19 (EOL) | 1.24 |

Net effect: fewer direct dependencies, a smaller binary, and every remaining dependency actively maintained.
