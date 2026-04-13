package dockerstandalone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

var (
	ErrBinaryNotFound       = errors.New(`"healthy" binary not found in container`)
	ErrProcessFailedToStart = errors.New("process failed to start in container")
	ErrFlagNotSupported     = errors.New("--health-check flag not supported in this container")
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
	err := execInContainer(ctx, cli, containerID, cmd)
	if err == nil {
		return nil
	}

	if isProcessFailedToStart(err) {
		return ErrProcessFailedToStart
	}

	return err
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

	// Read all the output so it blocks until the exec completes
	output, err := io.ReadAll(resp.Reader)
	if err != nil {
		return fmt.Errorf("reading exec output failed: %w", err)
	}

	inspect, err := cli.ContainerExecInspect(ctx, execIDResp.ID)
	if err != nil {
		return fmt.Errorf("exec inspect failed: %w", err)
	}

	if inspect.ExitCode != 0 {
		return fmt.Errorf("command failed (%d): %s", inspect.ExitCode, string(output))
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
