package generate

import (
	"fmt"
	"os/exec"
	"path"

	petname "github.com/dustinkirkland/golang-petname"
	bettererrors "github.com/xtuc/better-errors"

	"github.com/bytearena/ba/subcommand/build"
)

var (
	samples = map[string]string{
		"nodejs": "https://github.com/ByteArena/sample-nodejs-agent.git",
	}
)

func BashComplete() (string, error) {
	var out string

	for k, _ := range samples {
		out += fmt.Sprintf("%s\n", k)
	}

	return out, nil
}

func cloneRepo(dest, url string) (string, error) {
	cmd := exec.Command("git", "clone", url, dest)

	stdout, stderr := cmd.CombinedOutput()

	err := cmd.Run()

	if err != nil && stderr != nil {
		return string(stdout), stderr
	}

	cmd = exec.Command("rm", "-rf", path.Join(dest, "./.git"))

	stdout, stderr = cmd.CombinedOutput()

	err = cmd.Run()

	if err != nil && stderr != nil {
		return string(stdout), stderr
	}

	return "", nil
}

func Main(name string) (bool, error) {

	if name == "" {
		name = "unknown"
	}

	dest := petname.Generate(2, "-")

	if url, hasSample := samples[name]; hasSample {
		out, err := cloneRepo(dest, url)

		if err != nil {
			return false, bettererrors.
				NewFromErr(err).
				SetContext("error", out)
		}
	} else {
		berror := bettererrors.
			New("Unknown sample").
			SetContext("name", name)

		return true, berror
	}

	fmt.Println(dest, "has been created")

	// Build agent
	showUsage, err := build.Main(dest)

	if err != nil {
		berror := bettererrors.
			New("ba build failed").
			With(err)

		return showUsage, berror
	}

	return false, nil
}
