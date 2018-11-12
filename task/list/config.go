package tasklist

import (
	"github.com/brinick/ali-ci/task"
	"github.com/brinick/ali-prbuild"
)

// TaskList is a list of tasks to be run
type Tasks []task.Tasker

// This list must be in order of decreasing task priority
func Get() Tasks {
	return Tasks{
		prbuild.NewTask(),
	}
}
