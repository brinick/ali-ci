package taskrunner

import (
	"fmt"
	"runtime"
	"time"

	"github.com/brinick/ali-ci/task"
	"github.com/brinick/logging"
)

// ------------------------------------------------------------------

type selectedTask struct {
	index int
	t     task.Tasker
}

// ------------------------------------------------------------------

// New creates a TaskRunner instance
func New(ts []task.Tasker) *TaskRunner {
	return &TaskRunner{
		tasks: ts, // assumed ordered by decreasing task priority
		abort: make(chan struct{}, 1),
		done:  make(chan struct{}, 1),
	}
}

// TaskRunner wraps the tasks to run
type TaskRunner struct {
	tasks   []task.Tasker
	current *selectedTask
	abort   chan struct{}
	done    chan struct{}
}

// Done returns a channel to check if the task runner is done
func (tr *TaskRunner) Done() <-chan struct{} {
	return tr.done
}

// SetDone closes the done channel to signal that
// the task runner is finished
func (tr *TaskRunner) SetDone() {
	close(tr.done)
}

// SetAbort closes the abort channel to signal that
// the task runner should shutdown its current task, and exit
func (tr *TaskRunner) SetAbort() {
	close(tr.abort)
}

// Abort returns the abort channel
func (tr *TaskRunner) Abort() <-chan struct{} {
	return tr.abort
}

func (tr *TaskRunner) hasTasks() bool {
	return len(tr.tasks) > 0
}

// selectTask finds the next task to be run.
// A task will be selected and returned if it has work
// to be done, and is of higher priority than the current
// task or the current task is not running, or there is
// no current task. After each unfruitful check for an
// available task the function sleeps for 60 seconds.
// It returns only when a task is found, or an
// external abort signal is caught.
func (tr *TaskRunner) selectTask() *selectedTask {
	if !tr.hasTasks() {
		return nil
	}

	for {
		logging.Debug("TaskRunner - selectTask")
		select {
		case <-tr.Abort():
			return nil
		case <-time.After(30 * time.Second):
			// TODO: switch back to 60 secs
			var task *selectedTask

			switch len(tr.tasks) {
			case 1:
				task = tr.handleSingleTaskList()
			default:
				task = tr.handleMultipleTaskList()
			}

			if task != nil {
				return task
			}
		}
	}
}

func (tr *TaskRunner) handleSingleTaskList() *selectedTask {
	if tr.current == nil {
		return &selectedTask{
			index: 0,
			t:     tr.tasks[0],
		}
	}

	if !tr.current.t.IsWorking() {
		return tr.current
	}

	return nil
}

func (tr *TaskRunner) handleMultipleTaskList() *selectedTask {
	// This presumes that the task list is
	// sorted by decreasing priority
	for index, task := range tr.tasks {
		if tr.isHigherPriorityTask(index) && task.WantsToRun() {
			return &selectedTask{
				index: index,
				t:     task,
			}
		}
	}

	return nil
}

func (tr *TaskRunner) isHigherPriorityTask(index int) bool {
	return (tr.current == nil ||
		index < tr.current.index ||
		(index > tr.current.index && !tr.current.t.IsWorking()))
}

func (tr *TaskRunner) setAllTasksSeekingWork() {
	logging.Info("Setting all tasks to seek work state")
	for _, task := range tr.tasks {
		task.SetStateSeek()
	}
}

func (tr *TaskRunner) setAllTasksIdle() {
	logging.Info("Setting all tasks to idle state")
	for _, task := range tr.tasks {
		task.SetStateIdle()
	}
}

// switchToTask sets the current task, if it is active, to seek state,
// then sets the provided task to be the current task and starts it running
func (tr *TaskRunner) switchToTask(next *selectedTask) {
	if next == nil {
		return
	}

	// Set the current task from run to seek state - if it exists -
	// and switch the current task to the next one, just selected
	if tr.current != nil {
		// Switch from run state to seek state
		tr.current.t.SetStateSeek()
	}

	tr.current = next

	// Behind the scenes this sets the task to run state,
	// and launches a goroutine to execute the running
	tr.current.t.SetStateRun()
}

// Run sets the task runner looping over the tasks as they
// become available (and in priority order). Higher priority
// tasks with work to do will cause the currently running
// lower priority task to be set back to Seek state looking for work.
func (tr *TaskRunner) Run() {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("TaskRunner Run() panicked", logging.F("err", r))
			// debug.PrintStack()
			buf := make([]byte, 1<<16)
			runtime.Stack(buf, true)
			logging.Info(fmt.Sprintf("%s", buf))

			tr.setAllTasksIdle()
			tr.SetDone()
		}
	}()

	if !tr.hasTasks() {
		logging.Info("No tasks to run, exiting")
		tr.SetDone()
		return
	}

	logging.Info("Have tasks to run", logging.F("n", len(tr.tasks)))
	logging.Debug("Setting all tasks to seek work")

	// Set all tasks to state seek where they are
	// checking if they have work
	tr.setAllTasksSeekingWork()

	for {
		// We have no next task to run, let's select one.
		// The select will block until either we have one,
		// or an external abort signal is received
		if task := tr.selectTask(); task != nil {
			tr.switchToTask(task)
		} else {
			// nil task returned = external abort
			tr.setAllTasksIdle()
			break
		}

		select {
		case <-tr.Abort():
			// shutdown requested externally
			// so shutdown the currently running task, and exit
			logging.Error("Aborting running task")
			tr.current.t.SetAbort()
			<-tr.current.t.Done()
			break
		default:
			// Round we go again to the selectTask function.
		}
	}

	tr.SetDone()
}
