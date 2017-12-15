package mapcmd

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"

	"github.com/cheggaaa/pb"
	bettererrors "github.com/xtuc/better-errors"

	"github.com/bytearena/core/common/utils"
)

const (
	MANIFEST_URL = "https://dltrainer.bytearena.com/manifest.json"
)

type mapBundleType struct {
	Md5     string `json:"md5"`
	Url     string `json:"url"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Comment string `json:"comment"`
}

type manifestType struct {
	Maps []mapBundleType `json:"maps"`
}

func MapListAction() {

	err := ensureMapDir()
	if err != nil {
		utils.FailWith(err)
	}

	manifest, err := getLocalMapManifest()
	if err != nil {
		fmt.Println("No maps are available locally. Please run the `map update` command first.")
		os.Exit(1)
	}

	someOutdated := false
	someMissing := false

	fmt.Printf("%d maps in manifest.\n", len(manifest.Maps))
	fmt.Println("")

	for _, mapbundle := range manifest.Maps {

		mapBundleLocation := GetMapLocation(mapbundle.Name)
		mapChecksum, err := GetLocalMapChecksum(mapbundle)

		downloaded := true
		uptodate := true
		if err != nil {
			// Local map has never been downloaded
			downloaded = false
			uptodate = false
			someMissing = true
		}

		if downloaded && (mapChecksum != mapbundle.Md5) {
			uptodate = false
			someOutdated = true
		}

		fmt.Println(fmt.Sprintf("# %s", mapbundle.Title))
		fmt.Println(fmt.Sprintf("- Name    : %s (--map \"%s\")", mapbundle.Name, mapbundle.Name))
		fmt.Println(fmt.Sprintf("- Info    : %s", mapbundle.Comment))
		fmt.Println(fmt.Sprintf("- URL     : %s", mapbundle.Url))
		if downloaded {
			fmt.Println(fmt.Sprintf("- On disk : %s", mapBundleLocation))
		} else {
			fmt.Println(fmt.Sprintf("- On disk : Never fetched"))
		}

		if !uptodate {
			fmt.Println(fmt.Sprintf("- Status  : outdated"))
		}

		fmt.Println("")
	}

	if someMissing || someOutdated {
		fmt.Println("Some maps are not downloaded or outdated. Please run the `map update` command.")
	}
}

func MapUpdateAction(debug func(str string)) {

	err := ensureMapDir()
	if err != nil {
		utils.FailWith(err)
	}

	fmt.Println("Downloading map manifest from " + MANIFEST_URL)
	fmt.Println("")

	mapManifest, errManifest := FetchManifest(MANIFEST_URL)
	if errManifest != nil {
		utils.FailWith(errManifest)
	}

	for _, mapbundle := range mapManifest.Maps {

		fmt.Println(fmt.Sprintf("# Map \"%s\" (%s)", mapbundle.Name, mapbundle.Url))
		fmt.Println("")

		mapExistsLocally := true

		mapChecksum, err := GetLocalMapChecksum(mapbundle)
		if err != nil {
			// Local map has never been downloaded
			fmt.Println("Map does not exist locally; will have to be fetched.")
			mapExistsLocally = false
		}

		if !mapExistsLocally || mapChecksum != mapbundle.Md5 {

			if mapExistsLocally {
				fmt.Println("Local version exists, but is outdated; downloading the new version.")
			}

			fmt.Println("")
			err := DownloadMap(mapbundle)
			fmt.Println("")

			if err != nil {
				utils.FailWith(err)
			}

			fmt.Println("[OK] Map downloaded!")
		} else {
			fmt.Println("[OK] Map already up to date!")
		}

		fmt.Println("")
	}
}

func GetMapLocation(mapname string) string {
	mapsDir, err := utils.GetTrainerMapsDir()

	if err != nil {
		utils.FailWith(err)
	}

	return path.Join(mapsDir, mapname+".zip")
}

func GetLocalMapChecksum(bundle mapBundleType) (string, error) {
	file, err := getMapLocally(bundle)
	defer file.Close()

	if err != nil {
		return "", err
	}

	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		log.Fatal(err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func DownloadMap(mapbundle mapBundleType) error {

	head, errHead := http.Head(mapbundle.Url)
	head.Body.Close()

	if errHead != nil {
		return bettererrors.
			New("Could not get map "+mapbundle.Name).
			With(bettererrors.NewFromErr(errHead)).
			SetContext("url", mapbundle.Url)
	}

	if head.StatusCode != 200 {
		msg := fmt.Sprintf("Could not get map %s from %s: server returned code %s", mapbundle.Name, mapbundle.Url, head.Status)
		return bettererrors.New(msg)
	}

	fileSize := int(head.ContentLength)

	res, errGet := http.Get(mapbundle.Url)
	if errGet != nil {
		return bettererrors.
			New("Could not get map "+mapbundle.Name).
			With(errHead).
			SetContext("url", mapbundle.Url)
	}

	tmpFile, tmpFileErr := ioutil.TempFile("", "bamap")

	if tmpFileErr != nil {
		return bettererrors.
			New("Could not open tmpfile").
			With(tmpFileErr)
	}

	bar := pb.New(fileSize)
	bar.SetWidth(80)
	bar.Start()

	rd := bar.NewProxyReader(res.Body)
	_, tmpCopyErr := io.Copy(tmpFile, rd)

	if tmpCopyErr != nil {
		return bettererrors.
			New("Error during copy to tmp").
			With(tmpCopyErr).
			SetContext("filename", tmpFile.Name())
	}

	bar.Finish()

	// Actually writing on disk the buffer
	mapBundleDestinationPath := GetMapLocation(mapbundle.Name)
	removeErr := os.Remove(mapBundleDestinationPath)

	if removeErr != nil {
		return bettererrors.
			New("Could not delete mapbundle").
			With(removeErr).
			SetContext("name", mapbundle.Name).
			SetContext("location", mapBundleDestinationPath)
	}

	file, errOpen := os.OpenFile(mapBundleDestinationPath, os.O_WRONLY|os.O_CREATE, 0755)

	if errOpen != nil {
		return bettererrors.
			New("Could not open destination file for map").
			With(errOpen).
			SetContext("name", mapbundle.Name).
			SetContext("location", mapBundleDestinationPath)
	}

	tmpFile.Seek(0, 0)
	_, copyErr := io.Copy(file, tmpFile)

	if copyErr != nil {
		return bettererrors.
			New("Error during copy to tmp").
			With(copyErr).
			SetContext("filename", file.Name())
	}

	file.Close()

	tmpFile.Close()
	os.Remove(tmpFile.Name())

	return nil
}

func FetchManifest(manifesturl string) (manifestType, error) {

	var manifest manifestType

	res, err := http.Get(manifesturl)

	if err != nil {
		return manifest, bettererrors.
			New("Could not download manifest").
			With(bettererrors.NewFromErr(err)).
			SetContext("manifest url", MANIFEST_URL)
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		msg := fmt.Sprintf("Could not download manifest (%s): server returned code %s", MANIFEST_URL, res.Status)
		return manifest, bettererrors.New(msg)
	}

	data, _ := ioutil.ReadAll(res.Body)
	err = json.Unmarshal(data, &manifest)

	if err != nil {
		return manifest, bettererrors.
			New("Could not parse manifest").
			With(bettererrors.NewFromErr(err)).
			SetContext("manifest url", manifesturl)
	}

	// Persist the manifest locally
	manifestPath, err := utils.GetTrainerMapsManifestPath()
	err = ioutil.WriteFile(manifestPath, data, 0644)
	if err != nil {
		return manifest, bettererrors.
			New("Could not persist the manifest locally").
			With(bettererrors.NewFromErr(err)).
			SetContext("manifest path", manifestPath)
	}

	return manifest, nil
}

func getLocalMapManifest() (manifestType, error) {

	var manifest manifestType

	manifestPath, err := utils.GetTrainerMapsManifestPath()
	if err != nil {
		return manifest, err
	}

	manifestJSON, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}

	err = json.Unmarshal(manifestJSON, &manifest)
	if err != nil {
		return manifest, bettererrors.
			New("Could not parse manifest").
			With(bettererrors.NewFromErr(err)).
			SetContext("manifest path", manifestPath)
	}

	return manifest, nil
}

func getMapLocally(bundle mapBundleType) (*os.File, error) {

	bundleLocation := GetMapLocation(bundle.Name)
	f, err := os.OpenFile(bundleLocation, os.O_RDONLY, 0755)

	if err != nil {
		return nil, bettererrors.
			New("Could not open map file").
			With(bettererrors.NewFromErr(err)).
			SetContext("map file", bundleLocation)
	}

	return f, nil
}

func ensureMapDir() error {

	mapsDir, err := utils.GetTrainerMapsDir()
	if err != nil {
		return err
	}

	err = os.MkdirAll(mapsDir, os.ModePerm)
	if err != nil {
		return err
	}

	return nil
}
