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
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
)

const buildDesc = `
This command builds the container images referenced from a chart directory.
For example, 'helm build foo' will build artifacts in a chart.

	foo/
	  |
	  |- images.yaml   # information oabout images required by the chart

'helm build' takes a path to a chart for an argument. If the chart does not
contain a 'images.yaml' file, the command will exit with an error.
`

func init() {
	RootCommand.AddCommand(buildCmd)
}

var buildCmd = &cobra.Command{
	Use:   "build NAME",
	Short: "builds images for a chart with the given name",
	Long:  buildDesc,
	RunE:  runBuild,
}

var errBuildNoChart = errors.New("no chart found for building (missing Chart.yaml)")
var errBuildNoImages = errors.New("no images defined for chart (missing images.yaml)")

func runBuild(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("the name of the chart to build is required")
	}

	path := args[0]

	if _, err := os.Stat(filepath.Join(path, "Chart.yaml")); err != nil {
		return errBuildNoChart
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	return build(filepath.Join(path, "images.yaml"))

}

// ImageInfo contains metadata about images used in a chart
type ImageInfo struct {
	Name    string
	Ref     string
	Path    string
	AbsPath string
}

func imageInfo(path string) ([]*ImageInfo, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		puts("error: fail to read file, %q. err = %q", path, err)
		return nil, err
	}

	imgs := make(map[string]interface{})
	if err = yaml.Unmarshal(b, &imgs); err != nil {
		puts("error: yaml unmarshal fail: err = %q", err)
		return nil, err
	}

	images := []*ImageInfo{}

	for key, val := range imgs {
		switch v := val.(type) {
		case map[string]interface{}:
			absPath, _ := filepath.Abs(v["Path"].(string))
			images = append(images, &ImageInfo{
				Name:    key,
				Ref:     v["Image"].(string),
				Path:    v["Path"].(string),
				AbsPath: absPath,
			})
		default:
			puts("\tkey %q with unhandled type: %T", key, val)
		}
	}

	return images, nil
}

func build(path string) error {
	images, err := imageInfo(path)
	if err != nil {
		return err
	}
	for _, img := range images {
		buildImg(img)
	}
	return nil
}

func buildImg(img *ImageInfo) error {

	// docker execs want to be in the local directory
	initialDir, _ := os.Getwd()
	os.Chdir(img.Path)
	defer os.Chdir(initialDir)

	err := execCommand(img.Name, "docker", "build", "-t", img.Ref, img.Path)
	if err != nil {
		return err
	}
	err = execCommand(img.Name, "docker", "push", img.Ref)
	if err != nil {
		return err
	}

	return nil
}

func execCommand(prefix string, cmdName string, cmdArgs ...string) error {
	cmd := exec.Command(cmdName, cmdArgs...)
	cmdReader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(cmdReader)
	go func() {
		for scanner.Scan() {
			fmt.Printf("%s | %s\n", prefix, scanner.Text())
		}
	}()

	err = cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	return nil
}

func puts(format string, args ...interface{}) {
	fmt.Println(fmt.Sprintf(format, args...))
}
