package portainer

import "testing"

func TestValidateImageWithLicense(t *testing.T) {
	tests := []struct {
		name    string
		license string
		image   string
		want    string
	}{
		{
			name:    "valid type 2 license",
			license: "2-abc123",
			image:   "portainer-ee:2.18.4",
			want:    "portainer-ee:2.18.4",
		},
		{
			name:    "invalid license",
			license: "1-abc123",
			image:   "portainer-ee:2.18.4",
			want:    "portainer-ee:2.18.4",
		},
		{
			name:    "invalid image name",
			license: "3-abc123",
			image:   "invalid-image",
			want:    "invalid-image",
		},
		{
			name:    "invalid semver tag",
			license: "3-abc123",
			image:   "portainer-ee:invalid-tag",
			want:    "portainer-ee:invalid-tag",
		},
		{
			name:    "lower than minimum version",
			license: "3-abc123",
			image:   "portainer-ee:2.18.3",
			want:    "portainer-ee:2.18.4",
		},
		{
			name:    "higher than minimum version",
			license: "3-abc123",
			image:   "portainer-ee:2.18.5",
			want:    "portainer-ee:2.18.5",
		},
		{
			name:    "with repo name",
			license: "3-abc123",
			image:   "portainer/portainer-ee:2.18.2",
			want:    "portainer/portainer-ee:2.18.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateImageWithLicense(tt.license, tt.image); got != tt.want {
				t.Errorf("validateImageWithLicense() = %v, want %v", got, tt.want)
			}
		})
	}
}
