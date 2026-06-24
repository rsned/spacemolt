// Package tasks holds the overmind's assigned-task model, seed-file loader, and
// the in-memory store that matches pending tasks to idle workers.
package tasks

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TaskStatus is the lifecycle state of a task.
type TaskStatus string

const (
	StatusPending  TaskStatus = "pending"
	StatusAssigned TaskStatus = "assigned"
	StatusRunning  TaskStatus = "running"
	StatusDone     TaskStatus = "done"
	StatusFailed   TaskStatus = "failed"
)

// Task is one unit of assignable work.
type Task struct {
	ID           string            `yaml:"id"`
	Script       string            `yaml:"script"`
	Params       map[string]string `yaml:"params"`
	RoleRequired string            `yaml:"role_required"`
	AgentID      string            `yaml:"agent_id"` // optional pin

	Status     TaskStatus `yaml:"-"`
	AssignedTo string     `yaml:"-"`
}

type tasksFile struct {
	Tasks []Task `yaml:"tasks"`
}

// LoadTasks parses the seed file at path, validating each task and defaulting
// Status to pending. Duplicate or empty ids, and missing script/role_required,
// are errors.
func LoadTasks(path string) ([]Task, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tasks: read %s: %w", path, err)
	}
	var tf tasksFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		return nil, fmt.Errorf("tasks: parse %s: %w", path, err)
	}
	seen := make(map[string]bool, len(tf.Tasks))
	for i := range tf.Tasks {
		t := &tf.Tasks[i]
		switch {
		case t.ID == "":
			return nil, fmt.Errorf("tasks: task #%d has empty id", i)
		case seen[t.ID]:
			return nil, fmt.Errorf("tasks: duplicate id %q", t.ID)
		case t.Script == "":
			return nil, fmt.Errorf("tasks: task %q has empty script", t.ID)
		case t.RoleRequired == "":
			return nil, fmt.Errorf("tasks: task %q has empty role_required", t.ID)
		}
		seen[t.ID] = true
		t.Status = StatusPending
	}
	return tf.Tasks, nil
}
