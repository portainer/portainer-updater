package nomad

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func Update(ctx context.Context, nomadCli *api.Client, job *api.Job, task *api.Task, imageName string, scheduleId string) error {

	log.Info().
		Str("image", imageName).
		Str("task", task.Name).
		Str("schedule-id", scheduleId).
		Interface("task config", task.Config).
		Msg("Updating Portainer agent")

	job.Update = api.DefaultUpdateStrategy()
	task.Config["image"] = imageName

	response, _, err := nomadCli.Jobs().EnforceRegister(job, *job.JobModifyIndex, nil)

	// response, _, err := nomadCli.Jobs().Register(job, nil)
	if err != nil {
		return errors.WithMessage(err, "failed to register job")
	}

	log.Debug().
		Str("job", *job.Name).
		Str("warnings", response.Warnings).
		Msg("Job registered")

	allocations, _, err := nomadCli.Jobs().Allocations(*job.ID, false, &api.QueryOptions{WaitIndex: response.JobModifyIndex})
	if err != nil {
		return errors.WithMessage(err, "failed to get allocations for job")
	}

	for _, allocation := range allocations {
		if allocation.ClientStatus != api.AllocClientStatusPending {
			continue
		}

		log.Info().
			Str("allocation", allocation.ID).
			Msg("waiting for allocation to start")

		for {
			time.Sleep(1 * time.Second)

			log.Debug().
				Str("allocation", allocation.ID).
				Msg("polling allocation")

			allocation, _, err := nomadCli.Allocations().Info(allocation.ID, nil)
			if err != nil {
				return errors.WithMessage(err, "failed to get allocation info")
			}

			if allocation.ClientStatus == api.AllocClientStatusPending {
				continue
			}

			if allocation.ClientStatus == api.AllocClientStatusRunning {
				log.Debug().
					Str("allocation", allocation.ID).
					Str("status", allocation.ClientStatus).
					Msg("Allocation success")
				return nil
			}

			cancel := make(chan struct{})
			frames, errCh := nomadCli.AllocFS().Logs(allocation, false, task.Name, "stderr", "end", 10, cancel, nil)

			select {
			case err := <-errCh:
				return err
			default:
			}
			signalCh := make(chan os.Signal, 1)
			signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

			// Create a reader
			var r io.ReadCloser
			frameReader := api.NewFrameReader(frames, errCh, cancel)
			frameReader.SetUnblockTime(500 * time.Millisecond)
			r = frameReader

			go func() {
				<-signalCh

				// End the streaming
				r.Close()
			}()

			output := ""
			if b, err := io.ReadAll(r); err == nil {
				output = string(b)
			}

			log.Error().
				Str("allocation", allocation.ID).
				Str("status", allocation.ClientStatus).
				Str("output", output).
				Msg("Allocation failed")

			if allocation.ClientStatus == api.AllocClientStatusFailed {
				return errors.New("allocation failed")
			}

			if allocation.ClientStatus == api.AllocClientStatusLost {
				return errors.New("allocation lost")
			}

			if allocation.ClientStatus == api.AllocClientStatusComplete {
				return errors.New("allocation complete")
			}

			return errors.New("unknown allocation status")
		}
	}

	return errors.New("no allocations found")
}
