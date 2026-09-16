package api

import (
	"fmt"
	"net/http"
	"regexp"

	"skifity/internal/errdoc"
)

// versionPattern is what a release tag looks like. Anything else is refused
// rather than passed to the cluster, because this value becomes part of an
// image reference.
var versionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.\-]+)?$`)

// upgradePanel rolls the panel forward to another version.
//
// The panel upgrades itself the same way any other app is updated: by changing
// the image on its own Deployment and letting Kubernetes roll it out. That means
// a failed upgrade rolls back on its own, and the apps the panel manages keep
// running throughout, because nothing about them depends on the panel being up.
func (s *Server) upgradePanel(r *http.Request, target string) error {
	if target == "" {
		return errdoc.BadRequest("Enter the version to upgrade to, for example v1.2.0.")
	}
	if !versionPattern.MatchString(target) {
		return errdoc.BadRequest(fmt.Sprintf("%q is not a version number. Use a release tag such as v1.2.0.", target))
	}
	if s.cluster == nil {
		return errdoc.ClusterUnreachable(nil)
	}

	upgrader, ok := s.cluster.(interface {
		UpgradePanel(ctx contextType, version string) error
	})
	if !ok {
		return errdoc.New("upgrade.unsupported", "This panel cannot upgrade itself").
			WithCause("The panel is not running inside a cluster it can update, which is normal in development.").
			WithImpact("Nothing was changed.").
			WithFix("Upgrade by pulling the new image and restarting, or re-run the installer with the version you want.").
			WithStatus(http.StatusBadRequest)
	}
	if err := upgrader.UpgradePanel(r.Context(), target); err != nil {
		return errdoc.New("upgrade.failed", "The upgrade could not be started").
			WithCause("%s", err.Error()).
			WithImpact("The panel is still running the current version.").
			WithFix("Check that the version exists and that the cluster can pull its image.").
			WithStatus(http.StatusBadGateway).Retry()
	}
	return nil
}
