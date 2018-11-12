package main

import (
	"flag"
	ci "github.com/brinick/ali-ci"
)

func main() {
	var clargs ci.Args

	flag.StringVar(
		&clargs.Logclient,
		"logger",
		"logrus",
		"Name of the logging client to use. Available: logrus | null",
	)

	flag.StringVar(
		&clargs.Logformat,
		"logformat",
		"json",
		"Output logging format (json, text)",
	)

	flag.StringVar(
		&clargs.Loglevel,
		"loglevel",
		"info",
		"Logging level. Available: debug | info | error",
	)
	flag.StringVar(
		&clargs.Logfile,
		"logfile",
		"", // empty = send to stdout/err
		"Path to which logging should be output",
	)

	flag.StringVar(
		&clargs.Profile,
		"profile",
		"dev",
		"Select the profile: dev or prod (IGNORED FOR NOW)",
	)
	flag.Parse()

	ci.Launch(&clargs)
}
