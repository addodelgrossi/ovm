package cli

import (
	"strconv"
	"strings"
)

// jsonIntent records whether raw process arguments explicitly request JSON.
// Cobra stops parsing at the first malformed or unknown flag, so consulting
// only the bound bool makes failure output depend on flag order. The raw scan
// is deliberately command-aware and skips values consumed by known flags so a
// literal value equal to "--json" is not mistaken for an output request.
type jsonIntent struct {
	known     bool
	requested bool
}

func detectJSONIntent(rawArgs []string, command string, valueFlags ...string) jsonIntent {
	if rawArgs == nil {
		return jsonIntent{}
	}
	start := -1
	for i, arg := range rawArgs {
		if arg == "--" {
			break
		}
		if arg == command {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return jsonIntent{known: true}
	}
	consumesValue := make(map[string]struct{}, len(valueFlags))
	for _, flag := range valueFlags {
		consumesValue[flag] = struct{}{}
	}
	requested := false
	for i := start; i < len(rawArgs); i++ {
		arg := rawArgs[i]
		if arg == "--" {
			break
		}
		if _, ok := consumesValue[arg]; ok {
			if i+1 < len(rawArgs) {
				i++
			}
			continue
		}
		if arg == "--json" {
			requested = true
			continue
		}
		if strings.HasPrefix(arg, "--json=") {
			if value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json=")); err == nil {
				requested = value
			}
		}
	}
	return jsonIntent{known: true, requested: requested}
}

func (i jsonIntent) enabled(parsed bool) bool {
	if i.known {
		return i.requested
	}
	return parsed
}
