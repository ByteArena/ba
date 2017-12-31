package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/Masterminds/semver"
	"github.com/docker/docker/client"
	petname "github.com/dustinkirkland/golang-petname"
	bettererrors "github.com/xtuc/better-errors"

	"github.com/bytearena/ba/subcommand/build"
	"github.com/bytearena/core/common/dockerfile"
	"github.com/bytearena/core/common/types"
)

var (
	samples = map[string]string{
		"nodejs": "https://github.com/ByteArena/sampleagent-deathmatch-nodejs.git",
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

func generateManifestFile(manifest types.AgentManifest, dir string) error {
	filename := types.GetManifestLocation(dir)
	os.Remove(filename)

	file, openErr := os.Create(filename)

	defer file.Close()

	if openErr != nil {
		berror := bettererrors.
			NewFromErr(openErr).
			SetContext("Filename", filename)

		return berror
	}

	bytes, _ := json.MarshalIndent(manifest, "", "    ")

	_, writeErr := file.Write(bytes)

	if writeErr != nil {
		return bettererrors.NewFromErr(writeErr)
	}

	return nil
}

func Main(name string) (bool, error) {

	if name == "" {
		name = "unknown"
	}

	petname := petname.Generate(2, "-")
	dest := petname

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

	// Update manifest file
	manifest, parseerror := types.ParseAgentManifestFromDir(dest)

	if parseerror != nil {
		berror := bettererrors.
			New("Could not parse manifest").
			With(parseerror)

		return false, berror
	}

	manifest.Id = petname
	manifest.Name = petname
	manifest.RepoURL = ""

	generationErr := generateManifestFile(manifest, dest)

	if generationErr != nil {
		berror := bettererrors.
			New("Manifest generation error").
			With(generationErr)

		return false, berror
	}

	// assert Docker supports multistaged
	assertErr := assertDockerSupportsMultiStageBuild(path.Join(dest, build.DOCKER_BUILD_FILE))

	if assertErr != nil {
		berror := bettererrors.
			New("assert Docker version failed").
			With(assertErr)

		return false, berror
	}

	// Build agent
	showUsage, err := build.Main(dest, build.Arguments{})

	if err != nil {
		berror := bettererrors.
			New("ba build failed").
			With(err)

		return showUsage, berror
	}

	return false, nil
}

func assertDockerSupportsMultiStageBuild(dockerfileLocation string) error {
	reader, openErr := os.Open(dockerfileLocation)

	if openErr != nil {
		return bettererrors.NewFromErr(openErr)
	}

	froms, parseError := dockerfile.DockerfileParserGetFroms(reader)

	if parseError != nil {
		return bettererrors.NewFromErr(parseError)
	}

	// Abord here because it's not a multistage-build
	if len(froms) == 1 {
		return nil
	}

	cli, clientErr := client.NewEnvClient()

	if clientErr != nil {
		return bettererrors.NewFromErr(clientErr)
	}

	serverVersion, getVersionErr := cli.ServerVersion(context.Background())

	if getVersionErr != nil {
		return bettererrors.NewFromErr(getVersionErr)
	}

	expectedRange := ">=17.05.0-ce"
	semverExpectedRange, _ := semver.NewConstraint(expectedRange)
	current, cliVersionError := semver.NewVersion(serverVersion.Version)

	if cliVersionError != nil {
		return bettererrors.NewFromErr(cliVersionError)
	}

	if ok, _ := semverExpectedRange.Validate(current); !ok {
		return bettererrors.
			New("Invalid Docker version").
			SetContext("expected", expectedRange).
			SetContext("current", serverVersion.Version)
	}

	return nil
}
