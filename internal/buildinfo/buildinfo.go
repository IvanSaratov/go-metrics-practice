// Package buildinfo выводит сведения о сборке приложения.
package buildinfo

import (
	"fmt"
	"io"
)

// Print выводит данные сборки, заменяя пустые значения на N/A.
func Print(out io.Writer, version, date, commit string) {
	if version == "" {
		version = "N/A"
	}
	if date == "" {
		date = "N/A"
	}
	if commit == "" {
		commit = "N/A"
	}
	fmt.Fprintf(out, "Build version: %s\nBuild date: %s\nBuild commit: %s\n", version, date, commit)
}
