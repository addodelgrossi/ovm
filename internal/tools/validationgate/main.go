// Command validationgate checks the physical-acceptance manifest of a release:
//
//	go run ./internal/tools/validationgate docs/validation/v0.1.0.json v0.1.0
//
// It exits 0 only when the manifest allows publishing the release.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/addodelgrossi/ovm/internal/release"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		_, _ = fmt.Fprintln(stderr, "usage: validationgate MANIFEST TAG")
		return 2
	}
	data, err := os.ReadFile(args[0]) //nolint:gosec // G703: a maintainer tool reading the manifest path it was given
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "release blocked: the validation manifest %s is missing or unreadable\n", args[0])
		return 1
	}
	manifest, problems := release.Validate(data, args[1])
	if len(problems) > 0 {
		_, _ = fmt.Fprintf(stderr, "release blocked: %v\n", release.ErrInvalid)
		for _, problem := range problems {
			_, _ = fmt.Fprintf(stderr, "- %v\n", problem)
		}
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "validation manifest accepted: %s with %d cameras\n", manifest.Release, len(manifest.Cameras))
	return 0
}
