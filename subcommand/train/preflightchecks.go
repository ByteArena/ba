package train

import (
	"os/exec"

	"github.com/bytearena/core/common/utils"
	bettererrors "github.com/xtuc/better-errors"
)

func runPreflightChecks() {
	ensureDockerIsAvailable()
}

func ensureDockerIsAvailable() {
	_, err := exec.LookPath("docker")

	if err != nil {
		utils.FailWith(
			bettererrors.New("Docker was not found in $PATH. Please install it."),
		)
	}
}
