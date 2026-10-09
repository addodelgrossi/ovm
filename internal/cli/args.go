package cli

import "github.com/spf13/cobra"

// usageArgs wraps a Cobra positional-args validator so a validation failure is
// tagged as a usage error and maps to the usage exit code. Cobra does not route
// argument errors through FlagErrorFunc, so they must be marked here.
func usageArgs(validator cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validator(cmd, args); err != nil {
			return markUsageError(err)
		}
		return nil
	}
}
