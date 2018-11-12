package task_test

import (
	"testing"

	"github.com/brinick/ali-ci/task"
)

func TestNewTask(t *testing.T) {
	name := "test_task"
	desc := "This is a test task"
	testTask := task.NewTask(name, desc)
	tname := testTask.Name()
	if tname != name {
		t.Fatalf(
			"Task name is incorrect, got %s, expected %s",
			tname,
			name,
		)
	}

	if testTask.IsWorking() {
		t.Fatal("Newly created task claims to be working")
	}

	if testTask.HaveWork {
		t.Fatal("Newly created task claims to have work")
	}
}
