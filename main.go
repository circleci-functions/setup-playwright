// Command setup-playwright installs and caches the browsers a project's
// Playwright needs, and runs a Playwright suite through CircleCI's testsuite,
// from a CircleCI job.
//
// Supported platforms: linux, darwin, windows.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct {
	Run     runCmd     `cmd:"" default:"withargs" help:"Install Playwright's browsers, then run a Playwright suite with circleci testsuite. Runs when no command is given."`
	Install installCmd `cmd:"" help:"Install the browsers, and their system packages, that the project's Playwright needs, caching them across jobs."`
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-playwright"),
		kong.Description("Install and cache Playwright's browsers, and run a Playwright suite through CircleCI's testsuite."),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run()
}
