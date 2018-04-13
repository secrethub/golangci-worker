package golinters

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/template"

	"github.com/golangci/golangci-worker/app/analytics"
	"github.com/golangci/golangci-worker/app/analyze/executors"
	"github.com/golangci/golangci-worker/app/analyze/linters/result"
)

type linterConfig struct {
	messageTemplate         *template.Template
	pattern                 *regexp.Regexp
	excludeByMessagePattern *regexp.Regexp
	args                    []string
	issuesFoundExitCode     int
}

func newLinterConfig(messageTemplate, pattern, excludeByMessagePattern string, args ...string) *linterConfig {
	if messageTemplate == "" {
		messageTemplate = "{{.message}}"
	}

	var excludeByMessagePatternRe *regexp.Regexp
	if excludeByMessagePattern != "" {
		excludeByMessagePatternRe = regexp.MustCompile(excludeByMessagePattern)
	}

	return &linterConfig{
		messageTemplate:         template.Must(template.New("message").Parse(messageTemplate)),
		pattern:                 regexp.MustCompile(pattern),
		excludeByMessagePattern: excludeByMessagePatternRe,
		args:                args,
		issuesFoundExitCode: 1,
	}
}

type linter struct {
	name string

	linterConfig
}

func newLinter(name string, cfg *linterConfig) *linter {
	return &linter{
		name:         name,
		linterConfig: *cfg,
	}
}

func (lint linter) Name() string {
	return lint.name
}

func (lint linter) doesExitCodeMeansIssuesWereFound(err error) bool {
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}

	status, ok := ee.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}

	exitCode := status.ExitStatus()
	return exitCode == lint.issuesFoundExitCode
}

func getOutTail(out string, linesCount int) string {
	lines := strings.Split(out, "\n")
	if len(lines) <= linesCount {
		return out
	}

	return strings.Join(lines[len(lines)-linesCount:], "\n")
}

func (lint linter) Run(ctx context.Context, exec executors.Executor) (*result.Result, error) {
	paths, err := getPathsForGoProject(exec.WorkDir())
	if err != nil {
		return nil, fmt.Errorf("can't get files to analyze: %s", err)
	}

	retIssues := []result.Issue{}

	const maxDirsPerRun = 100 // run one linter multiple times with groups of dirs: limit memory usage in the cost of higher CPU usage

	for len(paths.dirs) != 0 {
		args := append([]string{}, lint.args...)

		dirsCount := len(paths.dirs)
		if dirsCount > maxDirsPerRun {
			dirsCount = maxDirsPerRun
		}
		dirs := paths.dirs[:dirsCount]
		args = append(args, dirs...)

		out, err := exec.Run(ctx, lint.name, args...)
		if err != nil && !lint.doesExitCodeMeansIssuesWereFound(err) {
			out = getOutTail(out, 10)
			return nil, fmt.Errorf("can't run linter %s with args %v: %s, %s", lint.name, lint.args, err, out)
		}

		issues := lint.parseLinterOut(out)
		retIssues = append(retIssues, issues...)

		paths.dirs = paths.dirs[dirsCount:]
	}

	return &result.Result{
		Issues: retIssues,
	}, nil
}

type regexpVars map[string]string

func buildMatchedRegexpVars(match []string, pattern *regexp.Regexp) regexpVars {
	result := regexpVars{}
	for i, name := range pattern.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	return result
}

func (lint linter) parseLinterOutLine(line string) (regexpVars, error) {
	match := lint.pattern.FindStringSubmatch(line)
	if match == nil {
		return nil, fmt.Errorf("can't match line %q against regexp", line)
	}

	return buildMatchedRegexpVars(match, lint.pattern), nil
}

func (lint linter) makeIssue(vars regexpVars) (*result.Issue, error) {
	var messageBuffer bytes.Buffer
	err := lint.messageTemplate.Execute(&messageBuffer, vars)
	if err != nil {
		return nil, fmt.Errorf("can't execute message template: %s", err)
	}

	if vars["path"] == "" {
		return nil, fmt.Errorf("no path in vars %+v", vars)
	}

	var line int
	if vars["line"] != "" {
		line, err = strconv.Atoi(vars["line"])
		if err != nil {
			analytics.Log(context.TODO()).Warnf("Can't parse line %q: %s", vars["line"], err)
		}
	}

	return &result.Issue{
		FromLinter: lint.name,
		File:       vars["path"],
		LineNumber: line,
		Text:       messageBuffer.String(),
	}, nil
}

func (lint linter) parseLinterOut(out string) []result.Issue {
	issues := []result.Issue{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		vars, err := lint.parseLinterOutLine(scanner.Text())
		if err != nil {
			analytics.Log(context.TODO()).Warnf("Can't parse linter out line: %s", err)
			continue
		}

		message := vars["message"]
		ex := lint.excludeByMessagePattern
		if message != "" && ex != nil && ex.MatchString(message) {
			continue
		}

		issue, err := lint.makeIssue(vars)
		if err != nil {
			analytics.Log(context.TODO()).Warnf("Can't make issue: %s", err)
			continue
		}

		issues = append(issues, *issue)
	}

	return issues
}
