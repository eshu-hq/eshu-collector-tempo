// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector is the Eshu Tempo trace-signal collector entrypoint. With
// no arguments (or --sdk-stdio) it serves one claim: it reads one
// collector-sdk/v1alpha1 request from stdin and writes one result to stdout.
// With the seed subcommand it writes its own binary and manifest into a
// directory so an init container can install the component without a shell.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	collector "github.com/eshu-hq/eshu-collector-tempo"
	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, time.Now, nil); err != nil {
		fmt.Fprintf(os.Stderr, "collector: %v\n", err)
		os.Exit(1)
	}
}

// run dispatches on the first argument: "seed" installs the binary and
// manifest, anything else serves one claim.
func run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
	now func() time.Time,
	factory tempo.ClientFactory,
) error {
	if len(args) > 0 && args[0] == "seed" {
		return runSeed(args[1:], stdout, getenv)
	}
	return runClaim(ctx, args, stdin, stdout, stderr, getenv, now, factory)
}

func runClaim(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
	now func() time.Time,
	factory tempo.ClientFactory,
) error {
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// --sdk-stdio is the documented process-adapter argument. Serving one
	// claim from stdin is the only claim mode, so the flag only has to parse.
	flags.Bool("sdk-stdio", true, "read one collector SDK host request from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	request, err := collector.ParseRequest(stdin)
	if err != nil {
		return err
	}
	// The claim deadline is the initial lease expiry. The collector stops
	// early with a retryable result of its own instead of being killed
	// mid-request: it stops starting provider calls at the deadline minus
	// min(2s, 5% of the time left).
	runCtx, cancel, ok := claimContext(ctx, now(), request.Claim.Deadline)
	defer cancel()
	if !ok {
		return writeResult(stdout, collector.DeadlineExceededResult(request.Claim, now()))
	}
	handler := collector.Handler{Getenv: getenv, Now: now, ClientFactory: factory, Log: stderr}
	result := handler.Handle(runCtx, request)
	return writeResult(stdout, result)
}

// claimContext bounds one claim by its deadline minus a safety margin. It
// reports false when no deadline is left; the caller then fails the claim
// retryable without provider contact.
func claimContext(ctx context.Context, now time.Time, deadline time.Time) (context.Context, context.CancelFunc, bool) {
	if deadline.IsZero() {
		return ctx, func() {}, true
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return ctx, func() {}, false
	}
	margin := remaining / 20
	if margin > 2*time.Second {
		margin = 2 * time.Second
	}
	if margin >= remaining {
		return ctx, func() {}, false
	}
	bounded, cancel := context.WithTimeout(ctx, remaining-margin)
	return bounded, cancel, true
}

func writeResult(stdout io.Writer, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return errors.New("encode result")
	}
	if _, err := stdout.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}
