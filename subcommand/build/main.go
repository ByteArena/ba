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
	"path/filepath"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/term"
	"github.com/fsnotify/fsnotify"

	bettererrors "github.com/xtuc/better-errors"

	"github.com/bytearena/core/common/dockerfile"
	"github.com/bytearena/core/common/types"
	"github.com/bytearena/core/common/utils"
)

const (
	DOCKER_BUILD_FILE = "Dockerfile"
	SHOW_USAGE        = true
	DONT_SHOW_USAGE   = false

	WATCH_DIR_RECURSION_DEPTH = 100
)

type Arguments struct {
	WatchMode bool
}

type ImageLabels map[string]string

func welcomeBanner() {
	fmt.Println("=== ")
	fmt.Println("=== 🤖  Welcome! I'm the Byte Arena Builder Bot")
	fmt.Println("=== ")
	fmt.Println("")
}

func successBanner(id string) {
	fmt.Println("")
	fmt.Println("=== ")
	fmt.Println("=== ✅  Your agent has been built. Let'em know who's the best!")
	fmt.Println("===    Its id is: " + id)
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

func Main(dir string, args Arguments) (bool, error) {

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
	agentManifest, agentManifesterr := types.ParseAgentManifestFromDir(dir)

	if agentManifesterr != nil {
		return DONT_SHOW_USAGE, bettererrors.
			New("Failed to parse agent manifest").
			With(agentManifesterr)
	}

	agentManifestValiationErr := types.ValidateAgentManifest(agentManifest)

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

	id := agentManifest.Id

	labels := map[string]string{
		types.AGENT_MANIFEST_LABEL_KEY: agentManifest.String(),
	}

	if args.WatchMode {

		watcher, err := fsnotify.NewWatcher()

		if err != nil {
			return DONT_SHOW_USAGE, bettererrors.NewFromErr(err)
		}

		defer watcher.Close()

		waitChan := make(chan error, 1)
		awaitChangementIn(watcher, dir, waitChan)

		for {
			fmt.Println("=== Building your agent now.")
			fmt.Println("")

			err = runDockerBuild(cli, id, dir, labels)

			if err != nil {
				return DONT_SHOW_USAGE, err
			}

			successBanner(id)

			fmt.Printf("Awaiting changements in %s ...\n", dir)

			err = <-waitChan

			if err != nil {
				return DONT_SHOW_USAGE, err
			}
		}

	} else {

		fmt.Println("=== Building your agent now.")
		fmt.Println("")

		err = runDockerBuild(cli, id, dir, labels)

		if err != nil {
			return DONT_SHOW_USAGE, err
		}

		successBanner(id)

	}

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

	// from https://stackoverflow.com/a/40003617

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {

		if err != nil {
			return err
		}

		var link string
		if info.Mode()&os.ModeSymlink == os.ModeSymlink {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}

		header.Name = strings.TrimPrefix(path, basedir)
		if err = tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.Mode().IsRegular() { //nothing more to do for non-regular
			return nil
		}

		fh, err := os.Open(path)
		if err != nil {
			return err
		}
		defer fh.Close()

		if _, err = io.CopyBuffer(tw, fh, nil); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return err
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

func awaitChangementIn(watcher *fsnotify.Watcher, dir string, waitChan chan error) chan error {

	go func() {
		for {
			select {
			case event := <-watcher.Events:

				if event.Op&fsnotify.Write == fsnotify.Write {
					select {
					case waitChan <- nil: // ok
					default:
						fmt.Println("Already building ignoring")
					}
				}
			case err := <-watcher.Errors:
				waitChan <- bettererrors.NewFromErr(err)
				return
			}
		}
	}()

	err := watcher.Add(dir)

	if err != nil {
		waitChan <- bettererrors.NewFromErr(err)
		return waitChan
	}

	err = addDirWatchers(watcher, dir, 0)

	if err != nil {
		waitChan <- err
		return waitChan
	}

	return waitChan
}

func addDirWatchers(watcher *fsnotify.Watcher, dir string, detph uint) error {
	files, err := ioutil.ReadDir(dir)

	if err != nil {
		return bettererrors.NewFromErr(err)
	}

	for _, file := range files {
		if file.IsDir() {
			absName := path.Join(dir, file.Name())

			err := watcher.Add(absName)

			if err != nil {
				return bettererrors.NewFromErr(err)
			}

			if detph < WATCH_DIR_RECURSION_DEPTH {
				err := addDirWatchers(watcher, absName, detph+1)

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
