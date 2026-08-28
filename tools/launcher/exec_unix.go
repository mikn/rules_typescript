//go:build unix

package main

import "syscall"

// Exec replaces this process with argv so nothing lingers in the process tree.
func Exec(argv []string, env []string) error {
	return syscall.Exec(argv[0], argv, env)
}
