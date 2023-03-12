package dockerstandalone

import (
	"context"
	"encoding/base64"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
)

func loginEcrRegistry(ctx context.Context,
	dockerCli *client.Client,
	username,
	awsAuthorizationToken string,
) error {
	// Replace these values with your own
	// ecrRegistry := "<your-ecr-registry-url>"
	// ecrRegion := "<your-ecr-region>"
	// ecrImageName := "<your-ecr-image-name>"
	// awsAuthorizationToken := "<your-aws-authorization-token>"

	// Decode the AWS authorization token from base64
	decodedToken, err := base64.StdEncoding.DecodeString(awsAuthorizationToken)
	if err != nil {
		log.Err(err).
			Msg("failed to decode ECR authorized token")
		return err
	}

	// // Create a new Docker client
	// dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	// if err != nil {
	// 	return err
	// }

	// Set the ECR registry endpoint
	// dockerClient.NegotiateAPIVersion(context.Background())

	// Authenticate with the ECR registry using the AWS authorization token
	authConfig := types.AuthConfig{
		Username: username,
		Password: string(decodedToken),
	}
	_, err = dockerCli.RegistryLogin(context.Background(), authConfig)
	if err != nil {
		log.Err(err).
			Msg("failed to login ECR registry")
		return err
	}

	// Pull the image from the ECR registry
	// imageRef := fmt.Sprintf("%s/%s:%s", ecrRegistry, ecrImageName, ecrRegion)
	// out, err := dockerClient.ImagePull(context.Background(), imageRef, types.ImagePullOptions{})
	// if err != nil {
	// 	panic(err)
	// }
	// defer out.Close()

	// // Print the output of the pull command
	// output, err := ioutil.ReadAll(out)
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println(string(output))
	log.Err(err).
		Msg("Login ECR registry successfully")
	return nil
}
