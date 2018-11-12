package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/brinick/ali-ci/pullrequest"
	log "github.com/sirupsen/logrus"
)

type args struct {
	repo               string
	branch             string
	checkName          string
	status             string
	showMainBranch     bool
	trustedUsers       string
	trustCollaborators bool
	/*
		workerIndex        int
		workersPoolSize    int
	*/
}

func usage() {
	msg := `Usage:
	./main.go [-flags] repo@branch
where:
	repo is e.g. alisw/AliPhysics
	branch is e.g. master

Available flags:
`

	fmt.Fprintf(os.Stderr, msg)
	flag.PrintDefaults()
}

func parseArgs() args {
	// custom usage message
	flag.Usage = usage

	var clargs = args{}

	flag.StringVar(
		&clargs.checkName,
		"checkName",
		"build/AliPhysics/release",
		"The context string to match against head commit status",
	)

	flag.StringVar(
		&clargs.status,
		"status",
		"review",
		"The commit status which is considered trustworthy",
	)

	flag.BoolVar(
		&clargs.showMainBranch,
		"showMainBranch",
		false,
		"Show also for main branch tip, not just pull requests",
	)

	flag.StringVar(
		&clargs.trustedUsers,
		"trustedUsers",
		"",
		"Users whose requests can be trusted (comma-separated list)",
	)

	flag.BoolVar(
		&clargs.trustCollaborators,
		"trustCollaborators",
		false,
		"Trust all collaborators",
	)

	/*
		flag.IntVar(
			&clargs.workerIndex,
			"workerIndex",
			0,
			"Index for the current worker node",
		)

		flag.IntVar(
			&clargs.workersPoolSize,
			"workersPoolSize",
			1,
			"Total number of worker nodes",
		)
	*/

	flag.Parse()

	// Now we sort out the positional arg: repo@branch
	posArgs := flag.Args()
	if len(posArgs) != 1 {
		flag.Usage()
	} else {
		tokens := strings.Split(posArgs[0], "@")
		if len(tokens) > 2 {
			flag.Usage()
		} else {
			repo := tokens[0]
			branch := "master"
			if len(tokens) == 2 {
				branch = tokens[1]
			}

			clargs.repo = repo
			clargs.branch = branch
		}
	}

	return clargs
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	clargs := parseArgs()
	prs, err := pullrequest.Fetch(
		clargs.repo,
		clargs.branch,
		clargs.checkName,
		clargs.status,
		strings.Split(clargs.trustedUsers, ","),
		clargs.trustCollaborators,
		clargs.showMainBranch,
	)

	if err != nil {
		fmt.Println("Unable to fetch pull requests")
		fmt.Println(err)
		os.Exit(1)
	}

	for _, pr := range prs {
		log.Info(pr)
	}
}
