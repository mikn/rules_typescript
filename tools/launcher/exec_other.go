//go:build !unix

package main

import "os"

// Exec runs argv to completion where execve does not exist, then exits with the
// child's status so callers still observe exec-like semantics.
func Exec(argv []string, env []string) error {
	code, err := Supervise(argv, env, SuperviseOptions{})
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}
