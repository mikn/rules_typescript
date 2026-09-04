package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

// Environ layers the launcher's environment, the runfiles variables and the
// rule's env attribute, then drops the keys in unset. Duplicate keys are
// collapsed because execve keeps them all and getenv then answers with the
// first, inverting the precedence.
func Environ(env map[string]string, runfilesEnv []string) []string {
	value := map[string]string{}
	order := []string{}
	set := func(entry string) {
		key, val, found := strings.Cut(entry, "=")
		if !found {
			return
		}
		if _, seen := value[key]; !seen {
			order = append(order, key)
		}
		value[key] = val
	}
	for _, entry := range os.Environ() {
		set(entry)
	}
	for _, entry := range runfilesEnv {
		set(entry)
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		set(k + "=" + env[k])
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+value[k])
	}
	return out
}

// SuperviseOptions shapes how the launcher babysits a child it cannot exec into.
type SuperviseOptions struct {
	// IgnoreTerm keeps the child alive when the launcher is asked to terminate:
	// ibazel SIGTERMs on every rebuild and vite is meant to survive it.
	IgnoreTerm bool
	// ExitZeroOnInterrupt reports success after a Ctrl-C shutdown.
	ExitZeroOnInterrupt bool
	// Cleanup runs after the child exits, however it exits.
	Cleanup func()
}

// Supervise starts argv, forwards signals to it, and returns its exit status.
func Supervise(argv []string, env []string, opts SuperviseOptions) (int, error) {
	if opts.Cleanup != nil {
		defer opts.Cleanup()
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 1, err
	}

	forwarded := []os.Signal{os.Interrupt, syscall.SIGHUP}
	if !opts.IgnoreTerm {
		forwarded = append(forwarded, syscall.SIGTERM)
	} else {
		signal.Ignore(syscall.SIGTERM)
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, forwarded...)
	defer signal.Stop(sigs)

	interrupted := make(chan struct{}, 1)
	go func() {
		for s := range sigs {
			if s == os.Interrupt || s == syscall.SIGHUP {
				select {
				case interrupted <- struct{}{}:
				default:
				}
				_ = cmd.Process.Signal(syscall.SIGTERM)
				continue
			}
			_ = cmd.Process.Signal(s)
		}
	}()

	err := cmd.Wait()
	if opts.ExitZeroOnInterrupt {
		select {
		case <-interrupted:
			return 0, nil
		default:
		}
	}
	return exitCode(err)
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code >= 0 {
			return code, nil
		}
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return 1, nil
	}
	return 1, err
}
