package task

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"runtime"
	"time"

	"github.com/brinick/logging"
)

func thisDir() string {
	_, d, _, _ := runtime.Caller(0)
	return path.Dir(d)
}

func tasksDir() string {
	return thisDir()
}

// ------------------------------------------------------------------
// ------------------------------------------------------------------
// ------------------------------------------------------------------

const (
	idleState = "Idle" // doing no work
	seekState = "Seek" // looking for work
	runState  = "Run"  // doing work
)

// ------------------------------------------------------------------

type taskPreExecutor interface {
	PreExec(context.Context)
}

type taskExecutor interface {
	Exec(context.Context)
}

type taskPostExecutor interface {
	PostExec(context.Context)
}

type taskStateSeeker interface {
	SetStateSeek()
	EnterStateSeek()
	ExitStateSeek()
}

type taskStateRunner interface {
	SetStateRun()
	EnterStateRun()
	ExitStateRun()
}

type taskStateIdler interface {
	SetStateIdle()
	EnterStateIdle()
	ExitStateIdle()
}

type taskWorkFinder interface {
	FindWork(context.Context)
}

type aborter interface {
	SetAbort()
	Abort() <-chan struct{}
}

type finisher interface {
	SetDone()
	Done() <-chan struct{}
}

type runner interface {
	WantsToRun() bool
}

// ------------------------------------------------------------------

// Tasker defines the interface that a Task should satisfy
type Tasker interface {
	taskPreExecutor
	taskExecutor
	taskPostExecutor
	taskWorkFinder
	taskStateIdler
	taskStateSeeker
	taskStateRunner
	runner
	aborter
	finisher
	IsWorking() bool
	Name() string
}

// ------------------------------------------------------------------

var taskID uint

func createTaskID() uint {
	taskID++
	return taskID
}

// ------------------------------------------------------------------

type taskState struct {
	Name string
	When int64
}

func newState(name string) *taskState {
	return &taskState{
		Name: name,
		When: time.Now().Unix(),
	}
}

func (ts *taskState) is(state string) bool {
	return ts.Name == state
}

// ------------------------------------------------------------------

type stateHistory []*taskState

func (sh *stateHistory) add(state *taskState) {
	*sh = append(*sh, state)
}

// ------------------------------------------------------------------

// NewTask creates a new Task with the given name and description
func NewTask(name, desc string) *Task {
	now := time.Now().Unix()
	State := &taskState{idleState, now}

	t := &Task{
		name:          name,
		desc:          desc,
		id:            createTaskID(),
		created:       now,
		done:          make(chan struct{}),
		abort:         make(chan struct{}),
		stopWorker:    make(chan struct{}),
		stoppedWorker: make(chan struct{}),
		State:         State,
		history:       &stateHistory{State},
	}

	t.PreExecutor = t
	t.Executor = t
	t.PostExecutor = t
	t.WorkFinder = t
	return t
}

// ------------------------------------------------------------------

// Task represents the base task that other tasks should embed
// Particular interfaces may be overridden to tune to the child task.
type Task struct {
	name    string
	id      uint
	desc    string
	created int64
	err     error

	// Task overrideable interfaces
	PreExecutor  taskPreExecutor
	Executor     taskExecutor
	PostExecutor taskPostExecutor
	WorkFinder   taskWorkFinder

	// Current state and state history
	State   *taskState
	history *stateHistory

	// Closed by the task to indicate it is done
	done chan struct{}

	// If closed, tells the task to stop running
	abort chan struct{}

	// tell the task to stop running its worker go routine (either in
	// state seek or state run)
	stopWorker chan struct{}

	// check if the task has stopped running its worker go routine
	stoppedWorker chan struct{}

	// indicates that this task has work to do (used in state Seek)
	HaveWork bool
}

// ------------------------------------------------------------------

// Name retrieves the task name
func (t Task) Name() string {
	return t.name
}

// IsWorking checks if the internal stoppedWorker channel
// is closed, which would indicate that the task - whether
// it's in seek or run state - is not actively doing anything
func (t *Task) IsWorking() bool {
	if t.State.is(idleState) {
		return false
	}

	select {
	case <-t.stoppedWorker:
		return false
	default:
		return true
	}
}

// ------------------------------------------------------------------

// FindWork should be implemented by child tasks
func (t *Task) FindWork(ctx context.Context) {}

// ------------------------------------------------------------------

// Err returns the task error field
func (t *Task) Err() error {
	return t.err
}

// ------------------------------------------------------------------

// SetStateIdle sets the task to state idle
func (t *Task) SetStateIdle() {
	t.setState(idleState)
}

// ------------------------------------------------------------------

// SetStateSeek sets the task to state seek
func (t *Task) SetStateSeek() {
	t.setState(seekState)
}

// ------------------------------------------------------------------

// SetStateRun sets the task to state run
func (t *Task) SetStateRun() {
	t.setState(runState)
}

// ------------------------------------------------------------------

func (t *Task) setState(state string) {
	currentState := t.State.Name

	if currentState != state {

		logging.Debug(
			"Changing task state",
			logging.F("from", currentState),
			logging.F("to", state),
			logging.F("task", t.name),
		)

		// Exit the old state...
		t.execStateMethod("ExitState" + currentState)

		// ...log the new state...
		t.State = newState(state)
		t.history.add(t.State)

		// ...and enter it
		t.execStateMethod("EnterState" + state)
	}
}

// ------------------------------------------------------------------

// execStateMethod dynamically calls the task methods for the state
// with the given name
func (t *Task) execStateMethod(name string) {
	reflect.ValueOf(t).MethodByName(name).Call(nil) // []reflect.Value{}
	//t.err = values[0].Interface().(error)
}

// ------------------------------------------------------------------

func (t *Task) EnterStateIdle() {}
func (t *Task) ExitStateIdle()  {}

// ------------------------------------------------------------------

// EnterStateSeek launches a goroutine to periodically check
// if the task has new work to do. If it does, it signals on
// the task's HaveWork boolean.
func (t *Task) EnterStateSeek() {
	t.stopWorker = make(chan struct{})
	t.stoppedWorker = make(chan struct{})

	cancelCxt, cancel := context.WithCancel(context.Background())

	go func() {
		defer func() {
			cancel()
			close(t.stoppedWorker)
		}()

		done := make(chan struct{})
		panicked := make(chan struct{})

		// Inner goroutine that actually does the work
		go func() {
			defer func() {
				if r := recover(); r != nil {
					t.err = fmt.Errorf("FindWork: panicked (%s)", r)
					close(panicked)
				} else {
					logging.Debug("FindWork: done")
					close(done)
				}

			}()

			for {
				t.WorkFinder.FindWork(cancelCxt)

				duration := 42
				logging.Info(fmt.Sprintf("FindWork: sleep %ds", duration))
				select {
				// TODO: make this configurable, with 60 as default
				case <-time.After(time.Duration(duration) * time.Second):
					// wait 60 seconds before looking for work again
				case <-cancelCxt.Done():
					return
				}
			}
		}()

		select {
		case <-t.Abort():
			cancel()
		case <-t.stopWorker:
			logging.Debug("FindWork: shutting down")
			cancel()
		case <-panicked:
			t.HaveWork = false
			return
		case <-done:
			return
		}

		t.HaveWork = false

		// We are waiting for the inner worker to stop, given that
		// we cancelled the context. We'll give it a fixed time to clean
		// up and close the done channel, else we'll leave.
		select {
		case <-done:
			// all normal, bye...
		case <-time.After(5 * time.Minute):
			// inner worker considered stuck, let's leave
			// TODO: we should monitor for potentially stuck goroutines...
			// Maybe with expvars, prometheus, monalisa...
			logging.Info(
				"Child FindWork goroutine taking too long to close down after " +
					"context cancelled. Refusing to wait any longer, exiting parent. " +
					"Note: the child may still eventually exit, however it may be stuck.",
			)
			return
		}

	}()
}

func (t *Task) ExitStateSeek() {
	// indicate that the inner worker routine should stop
	close(t.stopWorker)

	// wait for the response that the worker has indeed stopped
	<-t.stoppedWorker
}

// ------------------------------------------------------------------

func (t *Task) EnterStateRun() {
	t.stopWorker = make(chan struct{})
	t.stoppedWorker = make(chan struct{})

	cancelCxt, cancel := context.WithCancel(context.Background())

	go func() {
		defer func() {
			cancel()
			close(t.stoppedWorker)
		}()

		done := make(chan struct{})
		panicked := make(chan struct{})

		// Inner goroutine that actually does the work
		go func() {
			defer func() {
				if r := recover(); r != nil {
					t.err = fmt.Errorf("Executor goroutine panicked: %s", r)
					close(panicked)
				} else {
					close(done)
				}
			}()

			logging.Debug("Running task PreExec() function")
			t.PreExecutor.PreExec(cancelCxt)

			select {
			case <-cancelCxt.Done():
				return
			default:
				// ok, run next stage
			}

			logging.Debug("Running task Exec() function")
			t.Executor.Exec(cancelCxt)
			select {
			case <-cancelCxt.Done():
				return
			default:
				// ok, run next stage
			}

			logging.Debug("Running task PostExec() function")
			t.PostExecutor.PostExec(cancelCxt)
		}()

		// Stop if:
		// - the task runner aborts this task
		// - the task runner changes the task state (which calls
		//   exitStateRun func, closing the stopWorker channel)
		// - the internal worker routine panicked (specific tasks are of course
		//   free to recover from the panic and not reach this point)
		// - the task is done running for now, and allows other tasks to run
		// In the first two cases, the internal worker routine will still be
		// active. The deferred context cancel takes care of this.
		select {
		case <-t.Abort():
			cancel()
		case <-t.stopWorker:
			cancel()
		case <-panicked:
			return
		case <-done:
			// the task has decided to stop running for now,
			// so we can run other lower priority tasks if we want
			return
		}

		// We are waiting for the inner worker to stop, given that
		// we cancelled the context. We'll give it a fixed time to clean
		// up and close the done channel, else we'll leave.
		select {
		case <-done:
			// all normal, bye...
		case <-time.After(5 * time.Minute):
			// inner worker considered stuck, let's leave
			// TODO: we should monitor for potentially stuck goroutines...
			// Maybe with expvars, prometheus, monalisa...
			// TODO: also, what if the task runner switches this task back to running
			// and the new Exec function clashes with the "stuck" one here...is this possible?
			// Avoidable?
			logging.Info(
				"Child Executor goroutine taking too long to close down after " +
					"context cancelled. Refusing to wait any longer, exiting parent. " +
					"Note: the child may still eventually exit, however it may be stuck.",
			)
			return
		}

	}()
}

func (t *Task) ExitStateRun() {
	close(t.stopWorker)
	<-t.stoppedWorker
}

// ------------------------------------------------------------------

// WantsToRun returns a boolean indicating if the
// task is in Seek State and has work to be done
func (t *Task) WantsToRun() bool {
	return t.State.Name == seekState && t.HaveWork
}

// ------------------------------------------------------------------

// PreExec is called before the main Exec function
func (t *Task) PreExec(ctx context.Context) {}

// Exec is where the main work of a task occurs
func (t *Task) Exec(ctx context.Context) {}

// PostExec is called after the main Exec function
func (t *Task) PostExec(ctx context.Context) {}

// ------------------------------------------------------------------

// IsActive checks if the given task is running
func (t *Task) IsActive() bool {
	return t.State.Name == runState
}

// ------------------------------------------------------------------

// SetAbort writes to the task abort channel on which the task is listening
func (t *Task) SetAbort() {
	close(t.abort)
}

// Abort indicates if the given task should stop
func (t *Task) Abort() <-chan struct{} {
	return t.abort
}

// ------------------------------------------------------------------

// SetDone marks this task as finished
func (t *Task) SetDone() {
	close(t.done)
}

// Done gets the done channel
func (t *Task) Done() <-chan struct{} {
	return t.done
}

// ------------------------------------------------------------------
