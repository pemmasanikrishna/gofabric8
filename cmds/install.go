/**
 * Copyright (C) 2015 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *         http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package cmds

import (
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/oauth2"

	"github.com/blang/semver"
	"github.com/fabric8io/gofabric8/util"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/util/homedir"
)

const (
	docker                   = "docker"
	dockerMachine            = "docker-machine"
	dockerMachineDownloadURL = "https://github.com/docker/machine/releases/download/"
	minishiftFlag            = "minishift"
	minishiftOwner           = "jimmidyson"
	minishift                = "minishift"
	minikube                 = "minikube"
	minishiftDownloadURL     = "https://github.com/jimmidyson/"
	kubectl                  = "kubectl"
	kubernetes               = "kubernetes"
	oc                       = "oc"
	binLocation              = ".fabric8/bin/"
	kubeDownloadURL          = "https://storage.googleapis.com/"
	ocTools                  = "openshift-origin-client-tools"
)

var (
	githubClient *github.Client
)

type downloadProperties struct {
	clientBinary   string
	kubeDistroOrg  string
	kubeDistroRepo string
	kubeBinary     string
	extraPath      string
	downloadURL    string
	isMiniShift    bool
}

// NewCmdInstall installs the dependencies to run the fabric8 microservices platform
func NewCmdInstall(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Installs the dependencies to locally run the fabric8 microservices platform",
		Long:  `Installs the dependencies to locally run the fabric8 microservices platform`,

		Run: func(cmd *cobra.Command, args []string) {
			isMinishift := cmd.Flags().Lookup(minishiftFlag).Value.String() == "true"
			install(isMinishift)
		},
	}
	cmd.PersistentFlags().Bool(minishiftFlag, false, "Install minishift rather than minikube")
	return cmd
}

func install(isMinishift bool) {

	writeFileLocation := getFabric8BinLocation()

	err := os.MkdirAll(writeFileLocation, 0700)
	if err != nil {
		util.Errorf("Unable to create directory to download files %s %v\n", writeFileLocation, err)
	}

	err = downloadDriver()
	if err != nil {
		util.Warnf("Unable to download driver %v\n", err)
	}

	d := getDownloadProperties(isMinishift)
	err = downloadKubernetes(d)
	if err != nil {
		util.Warnf("Unable to download kubernetes distro %v\n", err)
	}

	err = downloadKubectlClient()
	if err != nil {
		util.Warnf("Unable to download client %v\n", err)
	}

	if d.isMiniShift {
		err = downloadOpenShiftClient()
		if err != nil {
			util.Warnf("Unable to download client %v\n", err)
		}
	}

}
func downloadDriver() (err error) {

	if runtime.GOOS == "darwin" {
		util.Infof("fabric8 recommends OSX users use the xhyve driver\n")
		info, err := exec.Command("brew", "info", "docker-machine-driver-xhyve").Output()

		if err != nil || strings.Contains(string(info), "Not installed") {
			e := exec.Command("brew", "install", "docker-machine-driver-xhyve")
			e.Stdout = os.Stdout
			e.Stderr = os.Stderr
			err = e.Run()
			if err != nil {
				return err
			}

			out, err := exec.Command("brew", "--prefix").Output()
			if err != nil {
				return err
			}

			brewPrefix := strings.TrimSpace(string(out))

			file := string(brewPrefix) + "/opt/docker-machine-driver-xhyve/bin/docker-machine-driver-xhyve"
			e = exec.Command("sudo", "chown", "root:wheel", file)
			e.Stdout = os.Stdout
			e.Stderr = os.Stderr
			err = e.Run()
			if err != nil {
				return err
			}

			e = exec.Command("sudo", "chmod", "u+s", file)
			e.Stdout = os.Stdout
			e.Stderr = os.Stderr
			err = e.Run()
			if err != nil {
				return err
			}

			util.Success("xhyve driver installed\n")
		} else {
			util.Success("xhyve driver already installed\n")
		}

	} else if runtime.GOOS == "linux" {
		return errors.New("Driver install for " + runtime.GOOS + " not yet supported")
	}
	return nil
}

func downloadKubernetes(d downloadProperties) (err error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	if runtime.GOOS == "windows" {
		d.kubeBinary += ".exe"
	}

	_, err = exec.LookPath(d.kubeBinary)
	if err != nil {
		latestVersion, err := getLatestVersionFromGitHub(d.kubeDistroOrg, d.kubeDistroRepo)
		if err != nil {
			util.Errorf("Unable to get latest version for %s/%s %v", d.kubeDistroOrg, d.kubeDistroRepo, err)
			return err
		}

		kubeURL := fmt.Sprintf(d.downloadURL+d.kubeDistroRepo+"/releases/"+d.extraPath+"v%s/%s-%s-%s", latestVersion, d.kubeDistroRepo, os, arch)
		if runtime.GOOS == "windows" {
			kubeURL += ".exe"
		}
		util.Infof("Downloading %s...\n", kubeURL)

		writeFileLocation := getFabric8BinLocation()

		err = downloadFile(writeFileLocation+d.kubeBinary, kubeURL)
		if err != nil {
			util.Errorf("Unable to download file %s/%s %v", writeFileLocation+d.kubeBinary, kubeURL, err)
			return err
		}
		util.Successf("Downloaded %s\n", d.kubeBinary)
	} else {
		util.Successf("%s is already available on your PATH\n", d.kubeBinary)
	}

	return nil
}

func downloadKubectlClient() (err error) {

	os := runtime.GOOS
	arch := runtime.GOARCH

	_, err = exec.LookPath(kubectl)
	if err != nil {
		latestVersion, err := getLatestVersionFromGitHub(kubernetes, kubernetes)
		if err != nil {
			return fmt.Errorf("Unable to get latest version for %s/%s %v", kubernetes, kubernetes, err)
		}

		clientURL := fmt.Sprintf("https://storage.googleapis.com/kubernetes-release/release/v%s/bin/%s/%s/%s", latestVersion, os, arch, kubectl)
		if runtime.GOOS == "windows" {
			clientURL += ".exe"
		}

		util.Infof("Downloading %s...\n", clientURL)

		writeFileLocation := getFabric8BinLocation()

		err = downloadFile(writeFileLocation+kubectl, clientURL)
		if err != nil {
			util.Errorf("Unable to download file %s/%s %v", writeFileLocation+kubectl, clientURL, err)
			return err
		}
		util.Successf("Downloaded %s\n", kubectl)
	} else {
		util.Successf("%s is already available on your PATH\n", kubectl)
	}

	return nil
}

func downloadOpenShiftClient() (err error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	_, err = exec.LookPath("oc")
	if err != nil {

		// need to fix the version we download as not able to work out the oc sha in the URL yet
		sha := "565691c"
		latestVersion := "1.2.2"

		clientURL := fmt.Sprintf("https://github.com/openshift/origin/releases/download/v%s/openshift-origin-client-tools-v%s-%s", latestVersion, latestVersion, sha)

		switch runtime.GOOS {
		case "windows":
			clientURL += "-windows.zip"
		case "darwin":
			clientURL += "-mac.zip"
		default:
			clientURL += fmt.Sprintf(clientURL+"-%s-%s.tar.gz", os, arch)
		}

		util.Infof("Downloading %s...\n", clientURL)

		writeFileLocation := getFabric8BinLocation()

		err = downloadFile(writeFileLocation+oc+".zip", clientURL)
		if err != nil {
			util.Errorf("Unable to download file %s/%s %v", writeFileLocation+oc, clientURL, err)
			return err
		}

		switch runtime.GOOS {
		case "windows":
			err = unzip(writeFileLocation+oc+".zip", writeFileLocation+".")
			if err != nil {
				util.Errorf("Unable to unzip %s %v", writeFileLocation+oc+".zip", err)
				return err
			}
		case "darwin":
			err = unzip(writeFileLocation+oc+".zip", writeFileLocation+".")
			if err != nil {
				util.Errorf("Unable to unzip %s %v", writeFileLocation+oc+".zip", err)
				return err
			}
		default:
			err = unzip(writeFileLocation+oc+".tar.gz", writeFileLocation+".")
			if err != nil {
				util.Errorf("Unable to untar %s %v", writeFileLocation+oc+".tar.gz", err)
				return err
			}
		}

		util.Successf("Downloaded %s\n", oc)
	} else {
		util.Successf("%s is already available on your PATH\n", oc)
	}

	return nil
}

// download here until install and download binaries are supported in minishift
func downloadFile(filepath string, url string) (err error) {

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// make it executable
	os.Chmod(filepath, 0755)
	if err != nil {
		return err
	}
	return nil
}

// borrowed from minishift until it supports install / download binaries
func getLatestVersionFromGitHub(githubOwner, githubRepo string) (semver.Version, error) {
	if githubClient == nil {
		token := os.Getenv("GH_TOKEN")
		var tc *http.Client
		if len(token) > 0 {
			ts := oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: token},
			)
			tc = oauth2.NewClient(oauth2.NoContext, ts)
		}
		githubClient = github.NewClient(tc)
	}
	client := githubClient
	var (
		release *github.RepositoryRelease
		resp    *github.Response
		err     error
	)
	release, resp, err = client.Repositories.GetLatestRelease(githubOwner, githubRepo)
	if err != nil {
		return semver.Version{}, err
	}
	defer resp.Body.Close()
	latestVersionString := release.TagName
	if latestVersionString != nil {
		return semver.Make(strings.TrimPrefix(*latestVersionString, "v"))

	}
	return semver.Version{}, fmt.Errorf("Cannot get release name")
}

func isInstalled(isMinishift bool) bool {
	home := homedir.HomeDir()
	if home == "" {
		util.Fatalf("No user home environment variable found for OS %s", runtime.GOOS)
	}

	// check if we can find a local kube config file
	if _, err := os.Stat(home + "/.kube/config"); os.IsNotExist(err) {
		return false
	}

	// check for kubectl
	_, err := exec.LookPath(kubectl)
	if err != nil {
		return false
	}

	if isMinishift {
		// check for minishift
		_, err = exec.LookPath(minishift)
		if err != nil {
			return false
		}
		// check for oc client
		_, err = exec.LookPath(oc)
		if err != nil {
			return false
		}

	} else {
		// check for minikube
		_, err = exec.LookPath(minikube)
		if err != nil {
			return false
		}
	}

	return true
}

func getDownloadProperties(isMinishift bool) downloadProperties {
	d := downloadProperties{}

	if isMinishift {
		d.clientBinary = oc
		d.extraPath = "download/"
		d.kubeBinary = minishift
		d.downloadURL = minishiftDownloadURL
		d.kubeDistroOrg = minishiftOwner
		d.kubeDistroRepo = minishift
		d.isMiniShift = true

	} else {
		d.clientBinary = kubectl
		d.kubeBinary = minikube
		d.downloadURL = kubeDownloadURL
		d.kubeDistroOrg = kubernetes
		d.kubeDistroRepo = minikube
		d.isMiniShift = false
	}
	return d
}

func getFabric8BinLocation() string {
	home := homedir.HomeDir()
	if home == "" {
		util.Fatalf("No user home environment variable found for OS %s", runtime.GOOS)
	}
	return filepath.Join(home, binLocation)
}

func unzip(archive, target string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}

	for _, file := range reader.File {
		path := filepath.Join(target, file.Name)
		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return err
		}

		// make it executable
		os.Chmod(path, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}

func ungzip(source, target string) error {
	reader, err := os.Open(source)
	if err != nil {
		return err
	}
	defer reader.Close()

	archive, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer archive.Close()

	target = filepath.Join(target, archive.Name)
	writer, err := os.Create(target)
	if err != nil {
		return err
	}
	defer writer.Close()

	_, err = io.Copy(writer, archive)
	if err != nil {
		return err
	}

	// make it executable
	os.Chmod(target, 0755)
	if err != nil {
		return err
	}
	return err
}