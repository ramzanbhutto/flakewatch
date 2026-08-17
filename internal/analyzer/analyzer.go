// Package analyzer calls the Python extractor and parses its JSON output.
package analyzer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Result holds the categorized output from the Python extractor.
type Result struct {
	RunID string `json:"run_id"`
	Category string `json:"category"`
	Confidence float64 `json:"confidence"`
	Summary string `json:"summary"`
	FixHint string `json:"fix_hint"`
	FailureRegion string  `json:"failure_region"`
	Source string `json:"source"`
	Error string `json:"error,omitempty"`
}

// CategoryLabel returns a human-readable label for a category.
func CategoryLabel(cat string) string {
	labels := map[string]string{
		"infrastructure_blip": "Infrastructure Blip",
		"network_timeout": "Network Timeout",
		"race_condition": "Race Condition",
		"resource_exhaustion": "Resource Exhaustion",
		"genuine_regression": "Genuine Regression",
		"unknown": "Unknown",
	}
	if l, ok := labels[cat]; ok {
		return l
	}
	return cat
}

// Analyzer wraps the Python extractor script.
type Analyzer struct {
	scriptPath string
	token string
}

// NewAnalyzer creates an analyzer pointing at the extract.py script.
func NewAnalyzer(scriptPath, token string) *Analyzer {
	return &Analyzer{
		scriptPath: scriptPath,
		token: token,
	}
}

// ScriptDir returns the directory of this source file, used to locate scripts/.
func ScriptDir() string {
	_, filename, _, _ := runtime.Caller(0)
	// Go up two levels: internal/analyzer -> root
	return filepath.Join(filepath.Dir(filename), "..", "..", "scripts")
}

// Analyze calls extract.py for a given run ID and log URL.
func (a *Analyzer) Analyze(runID int64, logsURL string) (*Result, error) {
	cmd := exec.Command(
		"python3", a.scriptPath,
		"--logs-url", logsURL,
		"--run-id", fmt.Sprintf("%d", runID),
		"--token", a.token,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr= &stderr

	if err := cmd.Run(); err != nil {
		// Python script exited non-zero - parse error JSON if available
		var result Result
		if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr == nil {
			return &result, nil
		}
		return nil, fmt.Errorf("extract.py failed: %w\nstderr: %s", err, stderr.String())
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parse extractor output: %w\nraw: %s", err, stdout.String())
	}

	return &result, nil
}
