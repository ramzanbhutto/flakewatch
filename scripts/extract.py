#!/usr/bin/env python3
"""
extract.py -- Log extraction and LLM-based flake categorization.

Usage:
    python3 extract.py --logs-url <url> --run-id <id> --token <gh_token>

Output: JSON to stdout with keys:
    run_id, category, confidence, summary, failure_region
"""

import argparse
import io
import json
import os
import re
import sys
import zipfile
import urllib.request
import urllib.error
from typing import Optional


# Helper

FAILURE_PATTERNS = [
    (r"dial tcp.*connection refused", "network_timeout"),
    (r"lookup \S+ on \S+: dial udp.*unreachable", "network_timeout"),
    (r"net/http: request canceled", "network_timeout"),
    (r"context deadline exceeded", "network_timeout"),
    (r"EOF", "network_timeout"),
    (r"timeout", "network_timeout"),
    (r"OOMKilled|out of memory|memory limit", "resource_exhaustion"),
    (r"no space left on device", "resource_exhaustion"),
    (r"resource temporarily unavailable", "resource_exhaustion"),
    (r"race detected", "race_condition"),
    (r"DATA RACE", "race_condition"),
    (r"signal: killed", "infrastructure_blip"),
    (r"runner has received a shutdown signal", "infrastructure_blip"),
    (r"The runner has received a shutdown", "infrastructure_blip"),
    (r"Lost communication with the server", "infrastructure_blip"),
    (r"FATAL ERROR: CALL_AND_RETRY_LAST", "infrastructure_blip"),
]

MAX_REGION_LINES = 80   # lines of context to extract around failure
MAX_LOG_BYTES = 2*1024*1024  # 2MB max log to read


def fetch_log_text(logs_url: str, token: str) -> str:
    """Download the zip archive and extract the largest log file inside."""
    req = urllib.request.Request(logs_url)
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("User-Agent", "flakewatch/0.1.0")

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read(MAX_LOG_BYTES)
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"HTTP {e.code} fetching logs: {e.reason}")
    except urllib.error.URLError as e:
        raise RuntimeError(f"URL error fetching logs: {e.reason}")

    # Logs come as a zip archive
    try:
        zf = zipfile.ZipFile(io.BytesIO(raw))
    except zipfile.BadZipFile:
        # Sometimes the URL is a direct log, not a zip
        return raw.decode("utf-8", errors="replace")

    # Pick the largest file in the zip (most likely the main job log)
    entries = sorted(zf.infolist(), key=lambda e: e.file_size, reverse=True)
    if not entries:
        raise RuntimeError("zip archive is empty")

    with zf.open(entries[0]) as f:
        return f.read(MAX_LOG_BYTES).decode("utf-8", errors="replace")


def extract_failure_region(log_text: str) -> tuple[list[str], int]:
    """
    Find the first error/failure line and return surrounding context.
    Returns (lines, failure_line_index).
    """
    lines = log_text.splitlines()
    failure_idx = -1

    error_keywords = [
        "FAIL", "Error", "error", "FATAL", "panic", "fatal", "--- FAIL", "=== FAIL", "signal:", "timeout", "killed",
    ]

    for i, line in enumerate(lines):
        if any(kw in line for kw in error_keywords):
            failure_idx = i
            break

    if failure_idx == -1:
        # No obvious failure line -- return last N lines
        return lines[-MAX_REGION_LINES:], len(lines) - 1

    start = max(0, failure_idx - 20)
    end = min(len(lines), failure_idx + MAX_REGION_LINES)
    return lines[start:end], failure_idx


def classify_by_pattern(region_lines: list[str]) -> tuple[str, float]:
    """
    Fast regex-based classification before calling the LLM.
    Returns (category, confidence).
    """
    region_text = "\n".join(region_lines)
    for pattern, category in FAILURE_PATTERNS:
        if re.search(pattern, region_text, re.IGNORECASE):
            return category, 0.85
    return "unknown", 0.0

def classify_with_llm(region_text: str, run_id: str) -> Optional[dict]:
    """
    Call the Anthropic API to categorize the failure.
    Falls back to pattern-only result if API key is not set.
    """
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    if not api_key:
        return None

    import urllib.request as req_lib
    import json as json_lib

    prompt = f"""You are a CI flake analysis engine for the Podman container project.
Analyze this CI failure log excerpt from a GitHub Actions run (run ID: {run_id}).

Classify the failure into exactly one of these categories:
- infrastructure_blip: transient runner/cloud issue, not code-related
- network_timeout: external network, registry, or DNS failure
- race_condition: timing-dependent test failure
- resource_exhaustion: OOM, disk, CPU limit
- genuine_regression: actual code bug introduced by a commit
- unknown: cannot determine from the log

Respond ONLY with valid JSON, no markdown, no explanation:
{{
  "category": "<one of the six categories>",
  "confidence": <float 0.0 to 1.0>,
  "summary": "<one sentence plain English explanation>",
  "fix_hint": "<one sentence suggestion for the maintainer>"
}}

Log excerpt:
---
{region_text[:3000]}
---"""

    payload = json_lib.dumps({
        "model": "claude-sonnet-4-6",
        "max_tokens": 256,
        "messages": [{"role": "user", "content": prompt}],
    }).encode()

    request = req_lib.Request(
        "https://api.anthropic.com/v1/messages",
        data=payload,
        headers={
            "x-api-key": api_key,
            "anthropic-version": "2023-06-01",
            "content-type": "application/json",
        },
        method="POST",
    )

    try:
        with req_lib.urlopen(request, timeout=30) as resp:
            data = json_lib.loads(resp.read())
        text = data["content"][0]["text"].strip()
        # Strip markdown fences if present
        text = re.sub(r"^```[a-z]*\n?", "", text)
        text = re.sub(r"\n?```$", "", text)
        return json_lib.loads(text)
    except Exception as e:
        # LLM call failed - fall back to pattern result
        sys.stderr.write(f"LLM call failed: {e}\n")
        return None


# Main

def main():
    parser = argparse.ArgumentParser(description="Extract and categorize CI flake from log")
    parser.add_argument("--logs-url", required=True,  help="GitHub Actions logs download URL")
    parser.add_argument("--run-id",   required=True,  help="Workflow run ID")
    parser.add_argument("--token",    required=False, default="", help="GitHub token")
    args = parser.parse_args()

    try:
        log_text = fetch_log_text(args.logs_url, args.token)
    except RuntimeError as e:
        json.dump({"error": str(e), "run_id": args.run_id}, sys.stdout)
        sys.exit(1)

    region_lines, failure_idx = extract_failure_region(log_text)
    region_text = "\n".join(region_lines)

    # Try pattern-based classification first
    pattern_category, pattern_confidence = classify_by_pattern(region_lines)

    # Try LLM classification
    llm_result = classify_with_llm(region_text, args.run_id)

    if llm_result:
        result = {
            "run_id": args.run_id,
            "category": llm_result.get("category", pattern_category),
            "confidence": llm_result.get("confidence", pattern_confidence),
            "summary": llm_result.get("summary", ""),
            "fix_hint": llm_result.get("fix_hint", ""),
            "failure_region": region_text[:2000],
            "source": "llm",
        }
    else:
        # Pattern-only fallback
        result = {
            "run_id": args.run_id,
            "category": pattern_category,
            "confidence": pattern_confidence,
            "summary": f"Pattern match: {pattern_category}",
            "fix_hint": "",
            "failure_region": region_text[:2000],
            "source": "pattern",
        }

    json.dump(result, sys.stdout, indent=2)


if __name__ == "__main__":
    main()
