package main

import (
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/cmd/staticlint/noosexit"
	"github.com/gostaticanalysis/nilerr"
	"github.com/stretchr/testify/require"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple/s1002"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck/st1005"
)

func TestAnalyzers(t *testing.T) {
	checks := analyzers()
	require.NoError(t, analysis.Validate(checks))

	registered := make(map[string]*analysis.Analyzer, len(checks))
	for _, check := range checks {
		require.NotContains(t, registered, check.Name, "имя анализатора повторяется")
		registered[check.Name] = check
	}

	// В наборе должны остаться все стандартные проверки.
	standardNames := []string{
		"appends", "asmdecl", "assign", "atomic", "bools", "buildtag", "cgocall",
		"composites", "copylocks", "defers", "directive", "errorsas", "framepointer",
		"hostport", "httpresponse", "ifaceassert", "loopclosure", "lostcancel",
		"nilfunc", "printf", "shift", "sigchanyzer", "slog", "stdmethods", "stdversion",
		"stringintconv", "structtag", "testinggoroutine", "tests", "timeformat",
		"unmarshal", "unreachable", "unsafeptr", "unusedresult", "waitgroup",
	}
	for _, name := range standardNames {
		require.Contains(t, registered, name, "не подключена стандартная проверка")
	}

	for _, check := range staticcheck.Analyzers {
		require.Same(t, check.Analyzer, registered[check.Analyzer.Name], check.Analyzer.Name)
	}

	additional := []*analysis.Analyzer{
		s1002.Analyzer,
		st1005.Analyzer,
		bodyclose.Analyzer,
		nilerr.Analyzer,
		noosexit.Analyzer,
	}
	for _, check := range additional {
		require.Same(t, check, registered[check.Name], check.Name)
	}
}
