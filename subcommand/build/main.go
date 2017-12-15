package build

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/term"

	bettererrors "github.com/xtuc/better-errors"

	"github.com/bytearena/core/common/agentmanifest"
	"github.com/bytearena/core/common/dockerfile"
	"github.com/bytearena/core/common/utils"
)

const (
	DOCKER_BUILD_FILE = "Dockerfile"

	SHOW_USAGE      = true
	DONT_SHOW_USAGE = false
)

type ImageLabels map[string]string

func welcomeBanner() {
	fmt.Println("=== ")
	fmt.Println("=== 🤖  Welcome! I'm the Byte Arena Builder Bot (the local one)")
	fmt.Println("=== ")
	fmt.Println("")
}

func successBanner(name string) {
	fmt.Println("")
	fmt.Println("=== ")
	fmt.Println("=== ✅  Your agent has been built. Let'em know who's the best!")
	fmt.Println("===    Its name is: " + name)
	fmt.Println("=== ")
	fmt.Println("")
}

func BashComplete(dir string) (string, error) {
	var out string

	// No dir specified, using cwd instead
	if dir == "" {
		dir = "."
	}

	cwd, cwderr := os.Getwd()

	if cwderr != nil {
		return out, cwderr
	}

	// Transform to absolute dir
	dir = path.Join(cwd, dir)

	files, direrr := ioutil.ReadDir(dir)

	if direrr != nil {
		return out, direrr
	}

	for _, f := range files {

		if f.IsDir() {
			fqn := path.Join(dir, f.Name())

			if has, err := hasDockerBuildFile(fqn); has {
				if err != nil {
					return out, err
				}

				out += fmt.Sprintf("%s\n", f.Name())
			}
		}

	}

	return out, nil
}

func Main(dir string) (bool, error) {

	if dir == "" {

		// determine if current directory contains a Dockerfile
		pwd, err := os.Getwd()
		if err != nil {
			return SHOW_USAGE, bettererrors.New("No target directory was specified and there was an error determining the current working directory")
		}

		dir = pwd

		if has, err := hasDockerBuildFile(dir); !has || err != nil {
			return SHOW_USAGE, bettererrors.New("No target directory was specified, and the current directory does not contain any Dockerfile; is it really the source code of an agent?")
		}
	}

	if is, err := isDirectory(dir); !is {
		return SHOW_USAGE, err
	}

	if has, err := hasDockerBuildFile(dir); !has || err != nil {
		return SHOW_USAGE, bettererrors.New("The specified directory does not contain any Dockerfile; is it really the source code of an agent?")
	}

	// generate a labels map from the agent's ba.json
	agentManifest, agentManifesterr := agentmanifest.ParseFromDir(dir)

	if agentManifesterr != nil {
		return DONT_SHOW_USAGE, bettererrors.
			New("Failed to parse agent manifest").
			With(agentManifesterr)
	}

	agentManifestValiationErr := agentmanifest.Validate(agentManifest)

	if agentManifestValiationErr != nil {
		return DONT_SHOW_USAGE, bettererrors.
			New("Invalid agent manifest").
			With(agentManifestValiationErr)
	}

	cli, err := client.NewEnvClient()

	if err != nil {
		return DONT_SHOW_USAGE, bettererrors.
			New("Failed to initialize Docker").
			With(err)
	}

	welcomeBanner()

	fmt.Println("=== Building your agent now.")
	fmt.Println("")

	var name string

	// handles duilding . - https://github.com/ByteArena/cli/issues/8
	if dir == "." {
		cw, cwerr := os.Getwd()

		if cwerr != nil {
			return DONT_SHOW_USAGE, cwerr
		}

		name = path.Base(cw)
	} else {
		name = path.Base(dir)
	}

	labels := map[string]string{
		agentmanifest.AGENT_MANIFEST_LABEL_KEY: agentManifest.ToString(),
	}

	err = runDockerBuild(cli, name, dir, labels)

	if err != nil {
		return DONT_SHOW_USAGE, err
	}

	successBanner(name)

	return DONT_SHOW_USAGE, nil
}

func isDirectory(directory string) (bool, error) {

	if _, err := os.Stat(directory); os.IsNotExist(err) {

		return false, bettererrors.
			New("Directory does not exists").
			SetContext("directory", directory)
	} else {

		return true, nil
	}
}

func hasDockerBuildFile(inDirectory string) (bool, error) {

	if _, err := os.Stat(path.Join(inDirectory, DOCKER_BUILD_FILE)); os.IsNotExist(err) {

		return false, bettererrors.
			New("Docker build not found").
			SetContext("in directory", inDirectory).
			SetContext("file", DOCKER_BUILD_FILE)
	} else {

		return true, nil
	}
}

// Build a dir
// The dockerfile must be in the dir
func createTar(dir string) (io.Reader, error) {
	buff := bytes.NewBuffer(nil)
	tw := tar.NewWriter(buff)

	// Assert dockerfile
	dockerfileContents, err := ioutil.ReadFile(path.Join(dir, DOCKER_BUILD_FILE))

	if err != nil {
		return buff, err
	}

	err = failForbiddenInstructions(dockerfileContents)

	if err != nil {
		return buff, err
	}

	err = doTar(tw, dir, dir)

	if err != nil {
		return buff, err
	}

	return buff, nil
}

func doTar(tw *tar.Writer, dir string, basedir string) error {
	basedir = strings.TrimSuffix(basedir, "/") + "/"

	files, err := ioutil.ReadDir(dir)

	if err != nil {
		return err
	}

	for _, f := range files {
		fqn := path.Join(dir, f.Name())
		relpath := strings.TrimPrefix(fqn, basedir)

		if f.IsDir() {
			err := doTar(tw, fqn, basedir)

			if err != nil {
				return err
			}
		} else {
			tw.WriteHeader(&tar.Header{
				Name: relpath,
				Size: f.Size(),
				Mode: int64(f.Mode()),
			})

			contents, err := ioutil.ReadFile(fqn)
			_, err = tw.Write(contents)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func runDockerBuild(cli *client.Client, name, dir string, labels ImageLabels) error {
	ctx := context.Background()

	// TODO(sven): in addition of the name, we can add a tag to be able to list
	// our images. Useful in the bash autocomplete instead of listing the entire
	// local registry.
	opts := dockertypes.ImageBuildOptions{
		Tags:   []string{name},
		Labels: labels,
	}

	tar, tarErr := createTar(dir)
	if tarErr != nil {
		return tarErr
	}

	resp, err := cli.ImageBuild(ctx, tar, opts)

	if err != nil {
		return bettererrors.
			New("Docker build failed").
			With(err)
	}

	reader := resp.Body

	fd, isTerminal := term.GetFdInfo(os.Stdout)

	if err := jsonmessage.DisplayJSONMessagesStream(reader, os.Stdout, fd, isTerminal, nil); err != nil {
		return err
	}

	reader.Close()

	return nil
}

func failForbiddenInstructions(content []byte) error {
	forbiddenInstructions, err := dockerfile.DockerfileFindForbiddenInstructions(bytes.NewReader(content))

	if err != nil {
		return err
	}

	for name, _ := range forbiddenInstructions {
		berror := bettererrors.
			New("Forbidden instruction. Remember to remove it when you will to deploy your agent.").
			SetContext("name", name.String())

		utils.FailWith(berror)
	}

	return nil
}
