package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ManifestFile = "release_manifest.json"
)

type Service struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Version string `json:"version"`
}

type Manifest struct {
	ReleaseVersion string    `json:"release_version"`
	Services       []Service `json:"services"`
}

type DockerHubTags struct {
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
}

type IncrementType int

const (
	IncrementPatch IncrementType = iota
	IncrementMinor
	IncrementMajor
)

func main() {
	fmt.Println("Starting Releaser...")

	// 1. Load Manifest
	manifest, err := loadManifest(ManifestFile)
	if err != nil {
		fmt.Printf("Error loading manifest: %v\n", err)
		os.Exit(1)
	}

	updated := false
	maxIncrement := IncrementPatch

	for i, service := range manifest.Services {
		fmt.Printf("Checking service: %s (current: %s)\n", service.Name, service.Version)
		latestTag, err := getLatestTagFromDockerHub(service.Image)
		if err != nil {
			fmt.Printf("Error checking Docker Hub for %s: %v\n", service.Name, err)
			continue
		}

		if latestTag != service.Version && latestTag != "" {
			fmt.Printf("Found update for %s: %s -> %s\n", service.Name, service.Version, latestTag)

			incType := determineIncrementType(service.Version, latestTag)
			if incType > maxIncrement {
				maxIncrement = incType
			}

			manifest.Services[i].Version = latestTag
			updated = true
		} else {
			fmt.Printf("No update for %s\n", service.Name)
		}
	}

	if !updated {
		fmt.Println("No updates found.")
		return
	}

	// 2. Update Manifest File
	err = saveManifest(ManifestFile, manifest)
	if err != nil {
		fmt.Printf("Error saving manifest: %v\n", err)
		os.Exit(1)
	}

	// 3. Git Operations
	// Commit
	err = runGitCommand("add", ManifestFile)
	if err != nil {
		os.Exit(1)
	}

	msg := "chore: update services to latest versions"
	err = runGitCommand("commit", "-m", msg)
	if err != nil {
		os.Exit(1)
	}

	// Tag
	newVersion, err := generateNewVersion(maxIncrement)
	if err != nil {
		fmt.Printf("Error generating new version: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Creating new tag: %s\n", newVersion)

	// Update manifest with new version
	manifest.ReleaseVersion = newVersion
	err = saveManifest(ManifestFile, manifest)
	if err != nil {
		fmt.Printf("Error saving manifest with new version: %v\n", err)
		os.Exit(1)
	}

	// Commit again with the version update
	err = runGitCommand("add", ManifestFile)
	if err != nil {
		os.Exit(1)
	}

	msg = fmt.Sprintf("chore: release %s", newVersion)
	err = runGitCommand("commit", "-m", msg)
	if err != nil {
		os.Exit(1)
	}

	err = runGitCommand("tag", newVersion)
	if err != nil {
		os.Exit(1)
	}

	fmt.Println("Release created locally. Run 'git push --tags origin master' to publish.")
}

func loadManifest(path string) (*Manifest, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	err = json.Unmarshal(data, &m)
	return &m, err
}

func saveManifest(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, data, 0644)
}

func getLatestTagFromDockerHub(image string) (string, error) {
	parts := strings.Split(image, "/")
	if len(parts) == 1 {
		parts = []string{"library", parts[0]}
	}

	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/%s/tags?page_size=5", parts[0], parts[1])
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("docker hub api returned %d", resp.StatusCode)
	}

	var tags DockerHubTags
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", err
	}

	if len(tags.Results) == 0 {
		return "", nil
	}

	for _, tag := range tags.Results {
		if tag.Name != "latest" {
			return tag.Name, nil
		}
	}

	if len(tags.Results) > 0 {
		return tags.Results[0].Name, nil
	}

	return "", nil
}

func runGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Running: git %s\n", strings.Join(args, " "))
	return cmd.Run()
}

func parseVersion(v string) (major, minor, patch int, err error) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid version format: %s", v)
	}

	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return
	}

	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return
	}

	return
}

func determineIncrementType(oldVer, newVer string) IncrementType {
	oMaj, oMin, _, err1 := parseVersion(oldVer)
	nMaj, nMin, _, err2 := parseVersion(newVer)

	if err1 != nil || err2 != nil {
		// Fallback to patch if parsing fails (e.g. "latest")
		return IncrementPatch
	}

	if nMaj > oMaj {
		return IncrementMajor
	}
	if nMin > oMin {
		return IncrementMinor
	}
	return IncrementPatch
}

func generateNewVersion(incType IncrementType) (string, error) {
	year, week := time.Now().ISOWeek()
	prefix := fmt.Sprintf("v%d%02d", year, week) // e.g. v202452

	// Get existing tags
	cmd := exec.Command("git", "tag")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	tags := strings.Split(string(out), "\n")

	type version struct {
		minor, patch int
	}
	var versions []version

	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			// Parse vYYYYWW.Minor.Patch
			parts := strings.Split(tag, ".")
			if len(parts) >= 3 {
				minorStr := parts[1]
				patchStr := parts[2]

				m, err1 := strconv.Atoi(minorStr)
				p, err2 := strconv.Atoi(patchStr)

				if err1 == nil && err2 == nil {
					versions = append(versions, version{m, p})
				}
			}
		}
	}

	// Sort versions to find the latest
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].minor != versions[j].minor {
			return versions[i].minor < versions[j].minor
		}
		return versions[i].patch < versions[j].patch
	})

	currentMinor := 0
	currentPatch := -1 // So that if no tags exist, we start at 0

	if len(versions) > 0 {
		last := versions[len(versions)-1]
		currentMinor = last.minor
		currentPatch = last.patch
	}

	newMinor := currentMinor
	newPatch := currentPatch

	if incType == IncrementMinor || incType == IncrementMajor {
		newMinor++
		newPatch = 0
	} else {
		newPatch++
	}

	return fmt.Sprintf("%s.%d.%d", prefix, newMinor, newPatch), nil
}
