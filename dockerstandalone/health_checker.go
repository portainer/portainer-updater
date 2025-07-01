package dockerstandalone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

var ErrBinaryNotFound = errors.New(`"healthy" binary not found in container`)

type healthChecker struct{}

var defaultHealthChecker healthChecker

var healthyBinary = func() string {
	binary := "healthy"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	return binary
}()

func (f healthChecker) healthy(ctx context.Context, cli *client.Client, containerID string) error {
	cmd := []string{healthyBinary}

	if err := f.execInContainer(ctx, cli, containerID, cmd); err != nil {
		if f.isBinaryNotFoundError(err) {
			return ErrBinaryNotFound
		}

		return err
	}

	return nil
}

func (f healthChecker) execInContainer(ctx context.Context, cli *client.Client, containerID string, cmd []string) error {
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

	// Wait for command to complete by polling inspect
	for range 10 {
		inspect, err := cli.ContainerExecInspect(ctx, execIDResp.ID)
		if err != nil {
			return fmt.Errorf("exec inspect failed: %w", err)
		}
		if !inspect.Running {
			if inspect.ExitCode == 0 {
				return nil
			}
			output, err := io.ReadAll(resp.Reader)
			if err != nil {
				return fmt.Errorf("reading exec output failed: %w", err)
			}

			return fmt.Errorf("command failed (%d): %s", inspect.ExitCode, string(output))
		}

		time.Sleep(time.Second)
	}

	return fmt.Errorf("exec command timed out")
}

func (f healthChecker) isBinaryNotFoundError(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unable to start container process")
}
