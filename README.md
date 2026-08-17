# flakewatch

Automated CI flake categorization for Podman and CNCF projects.

Built as part of a CNCF LFX Mentorship 2026 Term 3 application for the **Podman Container Tools: Agentic CI Flake Categorization and Analysis** project.

---

## What It Does

`flakewatch` fetches failed GitHub Actions workflow runs, extracts the failure region from the logs, and categorizes each failure into one of six categories:

| Category              | Meaning                                           |
|-----------------------|---------------------------------------------------|
| `infrastructure_blip` | Transient runner or cloud issue, not code-related |
| `network_timeout`     | External registry, DNS, or network failure        |
| `race_condition`      | Timing-dependent test failure                     |
| `resource_exhaustion` | OOM, disk full, or CPU limit hit                  |
| `genuine_regression`  | Actual code bug introduced by a commit            |
| `unknown`             | Cannot determine from the log                     |

Categorization is done in two stages:

1. **Pattern matching** (fast, no API key needed) - regex rules derived from real Podman CI flake history
2. **LLM analysis** (optional) - sends the failure region to Claude for a richer plain-English explanation and fix hint

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
- `GITHUB_TOKEN` -- required (see below)
- `ANTHROPIC_API_KEY` -- optional, enables LLM analysis

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

## Getting an Anthropic API Key (optional, for LLM analysis)

1. Go to **console.anthropic.com** and create an account
2. Click **API Keys** on the left sidebar
3. Click  **Create Key**
3. Copy the key

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
export ANTHROPIC_API_KEY=your_anthropic_key

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

---

## Example Output

```
────────────────────────────────────────────────────────────────────────
  flakewatch report  ·  2026-08-17 20:05 UTC
────────────────────────────────────────────────────────────────────────

  Total failed runs analyzed: 5

  Category Breakdown
  ────────────────────────────────────────
  Network Timeout          █ 1
  Unknown                  ████ 4

────────────────────────────────────────────────────────────────────────

  [3/5] Run #32044569797
        Workflow : Running Copilot Code Review
        Branch   : main
        Category : Network Timeout
        Confidence: 85%
        Summary  : Pattern match: network_timeout
        URL      : https://github.com/containers/podman/actions/runs/32044569797

        Failure region:
        │ 2026-08-17T16:10:55.9078221Z })")
        │ 2026-08-17T16:10:55.9079849Z printf '%s' "$secrets" | "$RUNNER_PATH/**
        │ ... (11 more lines)
```

---

## Real Podman Flake Patterns Included

Pattern rules in `extract.py` are based on actual flakes filed in the Podman issue tracker:

- `containers/podman#5336` -- `stopped` vs `exited` state race condition
- `containers/podman#25057` -- machine test timeouts from slow quay.io image pulls
- `containers/podman#6640` -- streaming output log test race condition
