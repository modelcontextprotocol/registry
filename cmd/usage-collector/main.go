// Command usage-collector records how the hosted registry was used on each
// full UTC day: it reads the gateway's request counters and access records,
// and writes the day's totals and breakdowns to Postgres for the dashboards.
//
// It is specific to the hosted instance and is not part of the registry
// server. See README.md in this directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/usage"
)

// Version info, injected at build time via ldflags.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// dayList collects --day values, which may repeat or be comma-separated.
type dayList []time.Time

func (d *dayList) String() string {
	names := make([]string, len(*d))
	for i, day := range *d {
		names[i] = day.Format("2006-01-02")
	}
	return strings.Join(names, ",")
}

func (d *dayList) Set(value string) error {
	for _, text := range strings.Split(value, ",") {
		day, err := time.Parse("2006-01-02", strings.TrimSpace(text))
		if err != nil {
			return errors.New("a day is written like 2026-11-19")
		}
		*d = append(*d, day)
	}
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	var days dayList
	flags := flag.NewFlagSet("usage-collector", flag.ContinueOnError)
	flags.Var(&days, "day", "UTC day to collect, such as 2026-11-19; repeat or separate with commas.\n"+
		"Without it, every day in the look-back window that is not stored yet is collected")
	markdownDir := flags.String("markdown-dir", "", "also write each day's report to <dir>/<day>.md")
	noStore := flags.Bool("no-store", false, "collect and report without connecting to any database (needs --day)")
	allowDegraded := flags.Bool("allow-degraded", false, "collect and store a day even if a source it needs was unavailable")
	resync := flags.Bool("resync-publishing", false, "read every published version again, not only the changed ones")
	showVersion := flags.Bool("version", false, "print version information and exit")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintf(os.Stdout, "usage-collector %s (commit %s, built %s)\n", Version, GitCommit, BuildTime)
		return 0
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}

	logger := log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
	settings, err := usage.LoadSettings()
	if err != nil {
		logger.Printf("settings: %v", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = usage.Run(ctx, settings, usage.Options{
		Days:             days,
		MarkdownDir:      *markdownDir,
		NoStore:          *noStore,
		AllowDegraded:    *allowDegraded,
		ResyncPublishing: *resync,
		Version:          Version,
		Stdout:           os.Stdout,
		Logf:             logger.Printf,
	})
	if err != nil {
		logger.Printf("usage collection failed:\n%v", err)
		return 1
	}
	return 0
}
