package dockerstandalone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

var (
	ErrBinaryNotFound       = errors.New(`"healthy" binary not found in container`)
	ErrProcessFailedToStart = errors.New("process failed to start in container")
	ErrFlagNotSupported     = errors.New("--health-check flag not supported in this container")
)

const (
	// execTimeout bounds how long we wait for a health-check command to finish
	// inside the container.
	execTimeout = 10 * time.Second

	// exitCodeCannotExecute is the conventional exit code for a command that was
	// found but could not be executed, i.e. the process never started.
	exitCodeCannotExecute = 126
)

type healthCheck func(ctx context.Context, cli *client.Client, containerID string) error

var agentHealthyBinary = func() string {
	binary := "healthy"
	// This does not ensure the binary is actually a Windows binary, but it relies on the high likelihood
	// that if the updater is running on Windows, the container is also Windows-based.
	// A more robust solution would be to inspect the agent container image OS.
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	return binary
}()

func portainerHealthy(ctx context.Context, cli *client.Client, containerID string) error {
	cmd := []string{"/portainer", "--health-check"}

	err := healthyWithCmd(ctx, cli, containerID, cmd)
	if errors.Is(err, ErrProcessFailedToStart) || isUnknownFlagError(err) {
		err = errors.Join(err, ErrFlagNotSupported)
	}

	return err
}

func agentHealthy(ctx context.Context, cli *client.Client, containerID string) error {
	cmd := []string{agentHealthyBinary}

	err := healthyWithCmd(ctx, cli, containerID, cmd)
	if errors.Is(err, ErrProcessFailedToStart) {
		err = errors.Join(err, ErrBinaryNotFound)
	}

	return err
}

func healthyWithCmd(ctx context.Context, cli *client.Client, containerID string, cmd []string) error {
	if err := execInContainer(ctx, cli, containerID, cmd); err != nil {
		if isProcessFailedToStart(err) {
			return ErrProcessFailedToStart
		}

		return err
	}

	return nil
}

func execInContainer(ctx context.Context, cli *client.Client, containerID string, cmd []string) error {
	execOptions := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execIDResp, err := cli.ContainerExecCreate(ctx, containerID, execOptions)
	if err != nil {
		return fmt.Errorf("exec create failed: %w", err)
	}

	resp, err := cli.ContainerExecAttach(ctx, execIDResp.ID, container.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("exec attach failed: %w", err)
	}
	defer resp.Close()

	// The hijacked stream closes once the exec has finished, or has failed to
	// start, so draining it is the authoritative completion signal.
	//
	// Do not poll ContainerExecInspect to decide this. An exec the daemon has
	// accepted but not yet started reports Running=false with ExitCode=0, which
	// is indistinguishable from a clean success, so inspecting first could
	// report a command that never ran as healthy and skip a rollback.
	if err := resp.Conn.SetReadDeadline(time.Now().Add(execTimeout)); err != nil {
		return fmt.Errorf("setting exec read deadline failed: %w", err)
	}

	output, err := io.ReadAll(resp.Reader)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return errors.New("exec command timed out")
		}

		return fmt.Errorf("reading exec output failed: %w", err)
	}

	inspect, err := cli.ContainerExecInspect(ctx, execIDResp.ID)
	if err != nil {
		return fmt.Errorf("exec inspect failed: %w", err)
	}

	if inspect.Running {
		return errors.New("exec command timed out")
	}

	if inspect.ExitCode != 0 {
		err := fmt.Errorf("command failed (%d): %s", inspect.ExitCode, string(output))
		if inspect.ExitCode == exitCodeCannotExecute {
			return errors.Join(err, ErrProcessFailedToStart)
		}

		return err
	}

	return nil
}

func isProcessFailedToStart(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unable to start container process")
}

func isUnknownFlagError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unknown long flag '--health-check'")
}
