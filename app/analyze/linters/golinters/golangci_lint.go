package golinters

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/golangci/golangci-lint/pkg/printers"
	"github.com/golangci/golangci-worker/app/analytics"
	"github.com/golangci/golangci-worker/app/analyze/linters/result"
	"github.com/golangci/golangci-worker/app/lib/errorutils"
	"github.com/golangci/golangci-worker/app/lib/executors"
)

type GolangciLint struct {
	PatchPath string
}

func (g GolangciLint) Name() string {
	return "golangci-lint"
}

func (g GolangciLint) Run(ctx context.Context, exec executors.Executor) (*result.Result, error) {
	exec = exec.WithEnv("GOLANGCI_COM_RUN", "1")

	args := []string{
		"run",
		"--out-format=json",
		"--issues-exit-code=0",
		"--print-welcome=false",
		"--deadline=5m",
		"--new=false",
		"--new-from-rev=",
		"--new-from-patch=" + g.PatchPath,
	}
	args = append(args, filepath.Join(exec.WorkDir(), "..."))

	out, runErr := exec.Run(ctx, g.Name(), args...)
	rawJSON := []byte(out)

	if runErr != nil {
		var res printers.JSONResult
		if jsonErr := json.Unmarshal(rawJSON, &res); jsonErr == nil && res.Report.Error != "" {
			return nil, &errorutils.InternalError{
				PublicDesc:  fmt.Sprintf("can't run golangci-lint: %s", res.Report.Error),
				PrivateDesc: fmt.Sprintf("can't run golangci-lint: %s, %s", res.Report.Error, runErr),
			}
		}

		return nil, &errorutils.InternalError{
			PublicDesc:  "can't run golangci-lint",
			PrivateDesc: fmt.Sprintf("can't run golangci-lint: %s, %s", runErr, out),
		}
	}

	var res printers.JSONResult
	if jsonErr := json.Unmarshal(rawJSON, &res); jsonErr != nil {
		return nil, &errorutils.InternalError{
			PublicDesc:  "can't run golangci-lint: invalid output json",
			PrivateDesc: fmt.Sprintf("can't run golangci-lint: can't parse json output %s: %s", out, jsonErr),
		}
	}

	if res.Report != nil && len(res.Report.Warnings) != 0 {
		analytics.Log(ctx).Infof("Got golangci-lint warnings: %#v", res.Report.Warnings)
	}

	var retIssues []result.Issue
	for _, i := range res.Issues {
		retIssues = append(retIssues, result.Issue{
			File:       i.FilePath(),
			LineNumber: i.Line(),
			Text:       i.Text,
			FromLinter: i.FromLinter,
			HunkPos:    i.HunkPos,
		})
	}
	return &result.Result{
		Issues:     retIssues,
		ResultJSON: json.RawMessage(rawJSON),
	}, nil
}
