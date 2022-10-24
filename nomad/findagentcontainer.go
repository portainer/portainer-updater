package nomad

import (
	"context"
	"strings"

	"github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
)

func FindAgentContainer(ctx context.Context, nomadCli *api.Client) (job *api.Job, task *api.Task, err error) {
	jobs, _, err := nomadCli.Jobs().List(nil)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "failed listing jobs")
	}

	for _, jobSummary := range jobs {
		if jobSummary.Type != "service" {
			continue
		}
		job, _, err := nomadCli.Jobs().Info(jobSummary.ID, nil)
		if err != nil {
			return nil, nil, errors.WithMessage(err, "failed to get job info")
		}

		for _, group := range job.TaskGroups {
			for _, task := range group.Tasks {
				if task.Driver != "docker" {
					continue
				}

				if taskImage, ok := task.Config["image"].(string); ok && (strings.HasPrefix(taskImage, "portainer/agent") || strings.HasPrefix(taskImage, "portainerci/agent")) {
					return job, task, nil
				}
			}
		}

	}

	return nil, nil, errors.New("no agent container found")
}
