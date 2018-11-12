package ci

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/brinick/ali-ci/task/list"
	"github.com/brinick/ali-ci/task/runner"
	"github.com/brinick/logging"
)

type Args struct {
	Logclient string
	Logformat string
	Loglevel  string
	Logfile   string
	Profile   string
}

func Launch(args *Args) {
	logging.SetClient(args.Logclient)
	logging.Configure(args.Loglevel, args.Logformat, args.Logfile)

	logging.Info("Creating the task runner")
	runner := taskrunner.New(tasklist.Get())

	logging.Debug("Launching the task runner")
	go runner.Run()

	// Now sit back and wait for either the runner to say it's done
	// or an external SIGTERM/SIGINT signal to be trapped
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)

	select {
	case <-runner.Done():
		// go home
		logging.Info("Task runner is done, exiting")
	case sig := <-signalChan:
		// received a signal, shut down everything
		logging.Info(
			"Signal caught, aborting the task runner and shutting down",
			logging.F("signal", sig),
		)
		runner.SetAbort()
		logging.Info("Waiting for task runner to exit")
		<-runner.Done()
		logging.Info("Done")
	}
}
