package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestRunDropsAllGroupsBeforeExecAndPreservesArguments(t *testing.T) {
	steps := []string{}
	uid, gid := 0, 0
	arguments := []string{"/app/server", "--mode", "local"}
	environment := []string{"SAFE=value"}
	execResult := errors.New("exec returned")
	operations := privilegeOperations{
		setGroups: func(groups []int) error {
			steps = append(steps, "groups")
			if len(groups) != 0 {
				t.Fatalf("groups = %v, want empty", groups)
			}
			return nil
		},
		setGID: func(value int) error {
			steps = append(steps, "gid")
			gid = value
			return nil
		},
		setUID: func(value int) error {
			steps = append(steps, "uid")
			uid = value
			return nil
		},
		getEGID: func() int { return gid },
		getEUID: func() int { return uid },
		exec: func(path string, receivedArguments, receivedEnvironment []string) error {
			steps = append(steps, "exec")
			if path != arguments[0] || !reflect.DeepEqual(receivedArguments, arguments) {
				t.Fatalf("unexpected exec: %q %v", path, receivedArguments)
			}
			if !reflect.DeepEqual(receivedEnvironment, environment) {
				t.Fatalf("environment = %v, want %v", receivedEnvironment, environment)
			}
			return execResult
		},
	}

	if err := run(arguments, environment, operations); !errors.Is(err, execResult) {
		t.Fatalf("run error = %v, want exec result", err)
	}
	if !reflect.DeepEqual(steps, []string{"groups", "gid", "uid", "exec"}) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestRunFailsClosedBeforeExec(t *testing.T) {
	tests := []struct {
		name       string
		arguments  []string
		initialUID int
		setGIDErr  error
	}{
		{name: "missing command", initialUID: 0},
		{name: "not root", arguments: []string{"/app/server"}, initialUID: 10001},
		{name: "set gid fails", arguments: []string{"/app/server"}, initialUID: 0, setGIDErr: errors.New("denied")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executed := false
			uid, gid := test.initialUID, 0
			operations := privilegeOperations{
				setGroups: func([]int) error { return nil },
				setGID: func(value int) error {
					if test.setGIDErr != nil {
						return test.setGIDErr
					}
					gid = value
					return nil
				},
				setUID:  func(value int) error { uid = value; return nil },
				getEGID: func() int { return gid },
				getEUID: func() int { return uid },
				exec: func(string, []string, []string) error {
					executed = true
					return nil
				},
			}
			if err := run(test.arguments, nil, operations); !errors.Is(err, errPrivilegeDrop) {
				t.Fatalf("run error = %v, want fail-closed", err)
			}
			if executed {
				t.Fatal("exec called after privilege failure")
			}
		})
	}
}
