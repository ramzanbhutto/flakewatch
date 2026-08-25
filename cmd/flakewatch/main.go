// flakewatch: Agentic CI flake categorization for public GitHub repos
// Environment variables:
//	GITHUB_TOKEN - required for private repos, avoids rate limiting
//	GROQ_API_KEY - optional, enables LLM-based categorization
package main

import(
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ramzanbhutto/flakewatch/internal/analyzer"
	"github.com/ramzanbhutto/flakewatch/internal/fetcher"
	"github.com/ramzanbhutto/flakewatch/internal/report"
)

func main(){
	owner := flag.String("owner", "containers", "GitHub repo owner")
	repo := flag.String("repo", "podman", "GitHub repo name")
	limit := flag.Int("limit", 10, "Max failed runs to analyze")
	script := flag.String("script", "", "Path to extract.py (auto-detected if empty)")
	flag.Parse()

	token := os.Getenv("GITHUB_TOKEN")
	if token == ""{
		fmt.Fprintln(os.Stderr, "warning: GITHUB_TOKEN not set - may hit rate limits")
	}

	// Auto-detect extract.py relative to binary location
	scriptPath:= *script
	if scriptPath == ""{
		exe, err := os.Executable()
		if err == nil {
			scriptPath = filepath.Join(filepath.Dir(exe), "scripts", "extract.py")
		} else{
			scriptPath = "scripts/extract.py"
		}
	}

	// Confirm script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: extract.py not found at %s\n", scriptPath)
		fmt.Fprintf(os.Stderr, " use --script to specify the path\n")
		os.Exit(1)
	}

	fmt.Printf("flakewatch starting\n")
	fmt.Printf(" target : %s/%s\n", *owner, *repo)
	fmt.Printf(" limit  : %d runs\n", *limit)
	fmt.Printf(" script : %s\n\n", scriptPath)

	// Step 1: fetch failed runs
	client := fetcher.NewClient(*owner, *repo, token)
	fmt.Printf("fetching failed workflow runs...\n")
	runs, err := client.FetchFailedRuns(*limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching runs: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("found %d failed runs\n\n", len(runs))

	if len(runs) == 0 {
		fmt.Println("no failed runs found - nothing to analyze")
		os.Exit(0)
	}

	// Step 2: fetch logs URL and analyze each run
	a := analyzer.NewAnalyzer(scriptPath, token)
	var reports []report.FlakeReport

	for i, run := range runs {
		fmt.Printf("[%d/%d] analyzing run #%d (%s)...\n", i+1, len(runs), run.ID, run.Name)

		logsURL, err := client.FetchLogsURL(run.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not get logs URL for run %d: %v\n", run.ID, err)
			reports= append(reports, report.FlakeReport{Run: run, Result: &analyzer.Result{
				RunID: fmt.Sprintf("%d", run.ID),
				Category: "unknown",
				Summary: "could not fetch logs: " + err.Error(),
			}})
			continue
		}

		result, err := a.Analyze(run.ID, logsURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: analysis failed for run %d: %v\n", run.ID, err)
			reports = append(reports, report.FlakeReport{Run: run, Result: &analyzer.Result{
				RunID: fmt.Sprintf("%d", run.ID),
				Category: "unknown",
				Summary: "analysis error: " + err.Error(),
			}})
			continue
		}

		fmt.Printf("  -> %-24s (%.0f%% confidence, source: %s)\n",
			analyzer.CategoryLabel(result.Category),
			result.Confidence*100,
			result.Source,
		)
		reports = append(reports, report.FlakeReport{Run: run, Result: result})
	}

	// Step 3: print report
	summary := report.NewSummary(reports)
	summary.PrintTerminal(os.Stdout)
}
