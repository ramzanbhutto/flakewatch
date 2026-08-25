# flakewatch

flakewatch is a CLI tool that fetches failed GitHub Actions runs from any public repo, pulls out the relevant failure region from each log and figures out whether it's a known flake pattern or an actual regression. It uses regex pattern matching first, with an optional LLM fallback for cases that need more context.

---

## What It Does

You point `flakewatch` at a repo and it goes and grabs the failed runs, digs through the logs for the part that actually broke, and sorts each one into six categories:

| Category              | Meaning                                           |
|-----------------------|---------------------------------------------------|
| `infrastructure_blip` | Transient runner or cloud issue, not code-related |
| `network_timeout`     | External registry, DNS, or network failure        |
| `race_condition`      | Timing-dependent test failure                     |
| `resource_exhaustion` | OOM, disk full, or CPU limit hit                  |
| `genuine_regression`  | Actual code bug introduced by a commit            |
| `unknown`             | Cannot determine from the log                     |

Categorization is done in two stages:

1. **Pattern matching** (fast, no API key needed) - regex rules derived from common CI flake patterns
2. **LLM analysis** (optional) - sends the failure region to LLM for a richer plain-English explanation and fix hint

---

## Architecture

```
flakewatch/
├── cmd/flakewatch/main.go          # Go CLI: orchestrates the pipeline
├── internal/
│   ├── fetcher/fetcher.go          # Go: GitHub Actions API client
│   ├── analyzer/analyzer.go        # Go: calls Python, parses result
│   └── report/report.go            # Go: formats terminal report
└── scripts/
    └── extract.py                  # Python: log download, extraction, LLM call
```

**Go** handles orchestration, API calls and reporting.
**Python** handles log extraction and LLM integration.

---

## Requirements

- Go 1.26.5+
- Python 3.9+
- `GITHUB_TOKEN` - required (see below)
- `GROQ_API_KEY` - optional, enables LLM analysis (free)

---

## Getting a GitHub Token

1. Go to **github.com -> Settings -> Developer settings -> Personal access tokens -> Tokens (classic)**
2. Click **Generate new token (classic)**
3. Give it a name (e.g. `flakewatch`)
4. Select these two scopes:
   - ✅ `repo`
   - ✅ `workflow`
5. Click **Generate token** and copy it

---

## Getting a Groq API Key (optional, for LLM analysis)

1. Go to **console.groq.com** and create an account
2. Click **API Keys** on the top bar 
3. Click **Create API Key** and copy it

Free tier is generous - no credit card needed.
Without this key, flakewatch still works using pattern matching only.

---

## Build

```bash
git clone https://github.com/ramzanbhutto/flakewatch
cd flakewatch
go build -o flakewatch ./cmd/flakewatch/
```

---

## Run
Quickest way to try it:

```bash
export GITHUB_TOKEN=your_github_token
./flakewatch
```

That's it - no flags needed. It defaults to the `containers/podman` repo, a limit of 10 runs, and auto-detects `scripts/extract.py` next to the binary. Pass `--owner`/`--repo` for a different project, or `--script` if you moved the binary somewhere else.

### Pattern-only mode (no LLM, no extra API key needed)

```bash
export GITHUB_TOKEN=your_github_token

./flakewatch \
  --owner containers \
  --repo podman \
  --limit 10 \
  --script scripts/extract.py
```

### Or with LLM categorization (richer output)

```bash
export GITHUB_TOKEN=your_github_token
export GROQ_API_KEY=your_groq_key

./flakewatch \
  --owner containers \
  --repo podman \
  --limit 10 \
  --script scripts/extract.py
```

### Flags

| Flag       | Default      | Description                        |
|------------|--------------|------------------------------------|
| `--owner`  | `containers` | GitHub repo owner                  |
| `--repo`   | `podman`     | GitHub repo name                   |
| `--limit`  | `10`         | Max failed runs to fetch           |
| `--script` | auto-detect  | Path to `extract.py`               |


> You can change `--limit` to fetch more or fewer runs.

---

## Example Output (limit:5)

```
────────────────────────────────────────────────────────────────────────
  flakewatch report  ·  2026-08-18 21:11 UTC
────────────────────────────────────────────────────────────────────────

  Total failed runs analyzed: 5

  Category Breakdown
  ────────────────────────────────────────
  Infrastructure Blip      █ 1
  Unknown                  ████ 4

────────────────────────────────────────────────────────────────────────

  [1/5] Run #32176110300
        Workflow : ci
        Branch   : main
        Category : Infrastructure Blip
        Confidence: 78%
        Summary  : The log is garbled binary output, indicating the CI runner
                   encountered an infrastructure-level failure.
        Fix hint : Rerun the job on a fresh runner and check for any runner
                   health issues.
        URL      : https://github.com/containers/podman/actions/runs/32176110300

  [3/5] Run #32144725978
        Workflow : ci
        Branch   : no-heading
        Category : Unknown
        Confidence: 78%
        Summary  : The provided log excerpt ends before any error appears,
                   making the root cause indiscernible.
        Fix hint : Include the portion of the log that contains the actual
                   failure message or test output.
        URL      : https://github.com/containers/podman/actions/runs/32144725978
```

---

## Flake Patterns Included

The regex rules in `extract.py` cover common CI flake patterns. A few of these came from real issues found while testing against `containers/podman`:

- `containers/podman#5336` -- `stopped` vs `exited` state race condition
- `containers/podman#25057` -- machine test timeouts from slow quay.io image pulls
- `containers/podman#6640` -- streaming output log test race condition
