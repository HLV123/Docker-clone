package container

import (
	"fmt"
	"os"
	"os/exec"

	"mydocker/internal/state"
)

// Exec vào namespace của container đang chạy
// Dùng nsenter system command để vào các namespaces
func Exec(containerID string, command []string) {
	s, err := state.Load(containerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "container not found: %s\n", containerID)
		os.Exit(1)
	}

	if s.Status != "running" {
		fmt.Fprintf(os.Stderr, "container %s is not running (status: %s)\n", containerID, s.Status)
		os.Exit(1)
	}

	if s.PID <= 0 {
		fmt.Fprintf(os.Stderr, "invalid PID for container %s\n", containerID)
		os.Exit(1)
	}

	// Dùng nsenter để vào tất cả namespaces của container
	// --target <pid>: process để lấy namespaces
	// --mount --uts --ipc --pid --net: các namespace cần vào
	// --: separator trước command
	nsenterArgs := []string{
		fmt.Sprintf("--target=%d", s.PID),
		"--mount",
		"--uts",
		"--ipc",
		"--pid",
		"--",
	}
	nsenterArgs = append(nsenterArgs, command...)

	cmd := exec.Command("nsenter", nsenterArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "nsenter: %v\n", err)
		os.Exit(1)
	}
}

// NsenterExec — không còn dùng nữa, giữ để tránh lỗi compile
func NsenterExec() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "nsenter-exec: no command")
		os.Exit(1)
	}
	command := os.Args[2]
	args := os.Args[2:]
	path, err := exec.LookPath(command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command not found: %s\n", command)
		os.Exit(1)
	}
	cmd := exec.Command(path, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
	}
}
