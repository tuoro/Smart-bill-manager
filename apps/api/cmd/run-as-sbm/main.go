package main

import (
	"errors"
	"os"
	"syscall"
)

const runtimeIdentity = 10001

var errPrivilegeDrop = errors.New("privilege drop failed")

type privilegeOperations struct {
	setGroups func([]int) error
	setGID    func(int) error
	setUID    func(int) error
	getEGID   func() int
	getEUID   func() int
	exec      func(string, []string, []string) error
}

func main() {
	operations := privilegeOperations{
		setGroups: syscall.Setgroups,
		setGID:    syscall.Setgid,
		setUID:    syscall.Setuid,
		getEGID:   os.Getegid,
		getEUID:   os.Geteuid,
		exec:      syscall.Exec,
	}
	if err := run(os.Args[1:], os.Environ(), operations); err != nil {
		_, _ = os.Stderr.WriteString("privilege_drop_failed\n")
		os.Exit(1)
	}
}

func run(arguments, environment []string, operations privilegeOperations) error {
	if len(arguments) == 0 || operations.getEUID() != 0 {
		return errPrivilegeDrop
	}
	if err := operations.setGroups([]int{}); err != nil {
		return errPrivilegeDrop
	}
	if err := operations.setGID(runtimeIdentity); err != nil {
		return errPrivilegeDrop
	}
	if err := operations.setUID(runtimeIdentity); err != nil {
		return errPrivilegeDrop
	}
	if operations.getEGID() != runtimeIdentity || operations.getEUID() != runtimeIdentity {
		return errPrivilegeDrop
	}
	return operations.exec(arguments[0], arguments, environment)
}
