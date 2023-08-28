package portainer

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/rs/zerolog/log"
)

// validateImageWithLicense validates the image name and tag based on the license type
func validateImageWithLicense(license, image string) string {
	if !strings.HasPrefix(license, "3-") {
		log.Debug().
			Str("license", license).
			Msg("License is a valid type 2 Portainer EE license, leaving it as is")
		return image
	}

	parts := strings.Split(image, ":")
	if len(parts) != 2 {
		log.Debug().
			Str("imageName", image).
			Msg("Image name is not a standard image (image:tag), leaving it as is")
		return image
	}

	imageName := parts[0]
	tag := parts[1]

	if !strings.HasSuffix(imageName, "portainer-ee") {
		log.Debug().
			Str("imageName", image).
			Msg("Image name is not portainer-ee, leaving it as is")
		return image
	}

	requiredVersion, err := semver.NewVersion(tag)
	if err != nil {
		log.Debug().
			Err(err).
			Str("tag", tag).
			Msg("Tag is not a valid semver, leaving it as is")
		return image
	}

	minVersion := "2.18.4"
	if requiredVersion.GreaterThan(semver.MustParse(minVersion)) {
		log.Debug().
			Str("tag", tag).
			Str("minVersion", minVersion).
			Msg("Tag is higher than minimum version, leaving it as is")
		return image
	}

	log.Info().
		Str("tag", tag).
		Str("minVersion", minVersion).
		Msg("Tag is lower than minimum version, updating version to 2.18.4")

	return fmt.Sprintf("%s:%s", imageName, minVersion)
}
