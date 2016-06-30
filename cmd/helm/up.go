/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"k8s.io/helm/pkg/helm"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

const upDesc = `
This command builds the container images referenced from a chart directory,
installs a release of the chart, and continues to re-build images and
update the release as changes are made on the local filesystem.

'helm up' operates from the current working directory and assumes a 'helm/'
subdirectory that contains the chart.
`

func init() {
	RootCommand.AddCommand(upCmd)
}

var upCmd = &cobra.Command{
	Use:               "up",
	Short:             "brings up the chart in local development mode",
	Long:              upDesc,
	RunE:              runUp,
	PersistentPreRunE: setupConnection,
}

var errUpNoChart = errors.New("no chart found (missing Chart.yaml)")

func runUp(cmd *cobra.Command, args []string) error {

	cwd, _ := os.Getwd()
	path := filepath.Join(cwd, "helm")

	if _, err := os.Stat(filepath.Join(path, "Chart.yaml")); err != nil {
		return errUpNoChart
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// enumerate images
	images, err := imageInfo(filepath.Join(path, "images.yaml"))
	if err != nil {
		return err
	}

	// initial build of images to ensure latest artifacts
	err = build(filepath.Join(path, "images.yaml"))
	if err != nil {
		return err
	}

	// initial install of release
	workingDir, _ := os.Getwd()
	releaseName := filepath.Base(workingDir)

	// read image values as config
	vals, err := ioutil.ReadFile(filepath.Join(path, "images.yaml"))
	if err != nil {
		return err
	}

	log.Printf("installing %s\n", releaseName)
	_, err = helm.InstallRelease(vals, releaseName, path, false)
	if err != nil {
		return prettyError(err)
	}
	log.Printf("%s installed\n", releaseName)

	return watchLoop(vals, releaseName, path, images)

}

func watchLoop(vals []byte, releaseName string, chartPath string, images []*ImageInfo) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				// ignore chmod, but rebuild on everything else
				if event.Op&fsnotify.Chmod == fsnotify.Chmod {
					continue
				}
				log.Println("fsnotify event on", event)
				rebuild(event.Name, images)
				log.Printf("uninstalling %s\n", releaseName)
				helm.UninstallRelease(releaseName, false)
				log.Printf("installing %s\n", releaseName)
				helm.InstallRelease(vals, releaseName, chartPath, false)
				log.Printf("%s installed\n", releaseName)
			case err := <-watcher.Errors:
				log.Println("fsnotify error:", err)
			}
		}
	}()

	// closure to add directory to watcher
	watchDir := func(path string, info os.FileInfo, err error) error {
		return watcher.Add(path)
	}
	// walk each image tree and watch all directories
	for _, img := range images {
		filepath.Walk(img.AbsPath, watchDir)
	}
	<-done
	return nil
}

func rebuild(eventPath string, images []*ImageInfo) {
	for _, img := range images {
		// see if event is relative path of an image
		_, err := filepath.Rel(img.AbsPath, eventPath)
		if err == nil {
			buildImg(img)
		}
	}
}
