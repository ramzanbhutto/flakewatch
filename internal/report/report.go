// Package report formats flake analysis results for terminal output.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ramzanbhutto/flakewatch/internal/analyzer"
	"github.com/ramzanbhutto/flakewatch/internal/fetcher"
)

// FlakeReport combines a run with its analysis result.
type FlakeReport struct {
	Run fetcher.Run
	Result *analyzer.Result
}

// Summary holds aggregated stats across all analyzed runs.
type Summary struct {
	Total int
	ByCategory map[string]int
	Reports []FlakeReport
	GeneratedAt time.Time
}

// NewSummary creates a summary from a list of flake reports.
func NewSummary(reports []FlakeReport) *Summary {
	s := &Summary{
		Total: len(reports),
		ByCategory: make(map[string]int),
		Reports: reports,
		GeneratedAt: time.Now().UTC(),
	}
	for _, r := range reports {
		if r.Result != nil {
			s.ByCategory[r.Result.Category]++
		}
	}
	return s
}

// PrintTerminal writes a human-readable report to w.
func (s *Summary) PrintTerminal(w io.Writer) {
	sep := strings.Repeat("─", 72)

	fmt.Fprintf(w, "\n%s\n", sep)
	fmt.Fprintf(w, "  flakewatch report  ·  %s\n", s.GeneratedAt.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(w, "%s\n\n", sep)
	fmt.Fprintf(w, "  Total failed runs analyzed: %d\n\n", s.Total)

	// Category breakdown
	fmt.Fprintf(w, "  Category Breakdown\n")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("-", 40))
	categories := []string{
		"infrastructure_blip",
		"network_timeout",
		"race_condition",
		"resource_exhaustion",
		"genuine_regression",
		"unknown",
	}
	for _, cat := range categories {
		count := s.ByCategory[cat]
		if count == 0 {
			continue
		}
		bar := strings.Repeat("█", count)
		fmt.Fprintf(w, "  %-24s %s %d\n", analyzer.CategoryLabel(cat), bar, count)
	}

	fmt.Fprintf(w, "\n%s\n\n", sep)

	// Individual run details
	for i, r := range s.Reports {
		if r.Result == nil {
			continue
		}
		fmt.Fprintf(w, "  [%d/%d] Run #%d\n", i+1, s.Total, r.Run.ID)
		fmt.Fprintf(w, "        Workflow : %s\n", r.Run.Name)
		fmt.Fprintf(w, "        Branch   : %s\n", r.Run.HeadBranch)
		fmt.Fprintf(w, "        Category : %s\n", analyzer.CategoryLabel(r.Result.Category))
		fmt.Fprintf(w, "        Confidence: %.0f%%\n", r.Result.Confidence*100)
		if r.Result.Summary != "" {
			fmt.Fprintf(w, "        Summary  : %s\n", r.Result.Summary)
		}
		if r.Result.FixHint != "" {
			fmt.Fprintf(w, "        Fix hint : %s\n", r.Result.FixHint)
		}
		fmt.Fprintf(w, "        URL      : %s\n", r.Run.HTMLURL)
		if r.Result.FailureRegion != "" {
			fmt.Fprintf(w, "\n        Failure region:\n")
			lines := strings.Split(r.Result.FailureRegion, "\n")
			limit := 10
			if len(lines) < limit {
				limit = len(lines)
			}
			for _, line := range lines[:limit] {
				fmt.Fprintf(w, "        │ %s\n", line)
			}
			if len(lines) > 10 {
				fmt.Fprintf(w, "        │ ... (%d more lines)\n", len(lines)-10)
			}
		}
		fmt.Fprintf(w, "\n%s\n\n", sep)
	}
}
