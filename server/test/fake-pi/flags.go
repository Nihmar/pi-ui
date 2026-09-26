package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// unsetMs marks an absent millisecond fault timer in config.
const unsetMs = -1

// config is the parsed command line: the harness knobs plus whatever pi argv the
// caller mixed into it.
type config struct {
	scriptPath  string
	emit        int
	rate        float64
	big         int
	crlf        bool
	separators  bool
	stderrLines int
	stallMs     int
	crashAfter  int
	exitAfter   int
	ignoreStdin bool
}

func usage(w io.Writer) {
	fmt.Fprint(w, "usage: fake-pi [--script FILE] [--emit N] [--rate R] [--big BYTES] [--crlf]\n"+
		"               [--separators] [--stderr LINES] [--stall-ms MS] [--crash-after MS]\n"+
		"               [--exit-after MS] [--ignore-stdin] [pi flags...]\n")
}

// parseArgs reads the harness flags and ignores every other argument, so callers pass
// the real `pi` argv unchanged. An unknown flag consumes the following argument as its
// value when that argument is not itself a flag, which covers pi's `--name x` and
// `-e path`; a bare positional (`rpc`) is ignored as well.
func parseArgs(args []string) (config, error) {
	cfg := config{crashAfter: unsetMs, exitAfter: unsetMs}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" || !strings.HasPrefix(arg, "-") {
			continue
		}
		name, inline, hasInline := strings.Cut(arg, "=")
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", name)
			}
			i++
			return args[i], nil
		}
		intFlag := func(target *int) error {
			raw, err := value()
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("flag %s: %q is not an integer", name, raw)
			}
			if n < 0 {
				return fmt.Errorf("flag %s: %d must not be negative", name, n)
			}
			*target = n
			return nil
		}

		var err error
		switch name {
		case "--script":
			cfg.scriptPath, err = value()
		case "--emit":
			err = intFlag(&cfg.emit)
		case "--big":
			err = intFlag(&cfg.big)
		case "--stderr":
			err = intFlag(&cfg.stderrLines)
		case "--stall-ms":
			err = intFlag(&cfg.stallMs)
		case "--crash-after":
			err = intFlag(&cfg.crashAfter)
		case "--exit-after":
			err = intFlag(&cfg.exitAfter)
		case "--rate":
			var raw string
			if raw, err = value(); err == nil {
				if cfg.rate, err = strconv.ParseFloat(raw, 64); err != nil {
					err = fmt.Errorf("flag --rate: %q is not a number", raw)
				} else if cfg.rate < 0 {
					err = fmt.Errorf("flag --rate: %v must not be negative", cfg.rate)
				}
			}
		case "--crlf":
			cfg.crlf = true
		case "--separators":
			cfg.separators = true
		case "--ignore-stdin":
			cfg.ignoreStdin = true
		default:
			if !hasInline && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		}
		if err != nil {
			return config{}, err
		}
	}
	return cfg, nil
}
