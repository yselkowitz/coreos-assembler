/*
	The RHCOS JobSpec is a YAML file describing the various Jenkins Job
	knobs for controlling Pipeline execution. The JobSpec pre-dates this
	code, and has been in production since 2019.

	The JobSpec has considerably more options than reflected in this file.

	Only include options that are believed to be relavent to COSA
*/

package spec

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// JobSpec is the root-level item for the JobSpec.
type JobSpec struct {
	Archives    Archives    `yaml:"archives,omitempty" json:"archives,omitempty"`
	CloudsCfgs  CloudsCfgs  `yaml:"clouds_cfgs,omitempty" json:"cloud_cofgs,omitempty"`
	Job         Job         `yaml:"job,omitempty" json:"job,omitempty"`
	Oscontainer Oscontainer `yaml:"oscontainer,omitempty" json:"oscontainer,omitempty"`
	Recipe      Recipe      `yaml:"recipe,omitempty" json:"recipe,omitempty"`
	Spec        Spec        `yaml:"spec,omitempty" json:"spec,omitempty"`

	// Stages are specific stages to be run. Stages are
	// only supported by Gangplank; they do not appear in the
	// Groovy Jenkins Scripts.
	Stages []Stage `yaml:"stages,omitempty" json:"stages,omitempty"`

	// DelayedMetaMerge ensures that 'cosa build' is called with
	// --delayed-meta-merge
	DelayedMetaMerge bool `yaml:"delay_meta_merge" json:"delay_meta_meta,omitempty"`
}

// Artifacts describe the expect build outputs.
//  All: name of the all the artifacts
//  Primary: Non-cloud builds
//  Clouds: Cloud publication stages.
type Artifacts struct {
	All     []string `yaml:"all,omitempty" json:"all,omitempty"`
	Primary []string `yaml:"primary,omitempty" json:"primary,omitempty"`
	Clouds  []string `yaml:"clouds,omitempty" json:"clouds,omitempty"`
}

// Aliyun is nested under CloudsCfgs and describes where
// the Aliyun/Alibaba artifacts should be uploaded to.
type Aliyun struct {
	Bucket  string   `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Enabled bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Regions []string `yaml:"regions,omitempty" json:"regions,omitempty"`
}

// Archives describes the location of artifacts to push to
//   Brew is a nested Brew struct
//   S3: publish to S3.
type Archives struct {
	Brew *Brew `yaml:"brew,omitempty" json:"brew,omitempty"`
	S3   *S3   `yaml:"s3,omitempty" json:"s3,omitempty"`
}

// Aws describes the upload options for AWS images
//  AmiPath: the bucket patch for pushing the AMI name
//  Public: when true, mark as public
//  Regions: name of AWS regions to push to.
type Aws struct {
	Enabled bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AmiPath string   `yaml:"ami_path,omitempty" json:"ami_path,omitempty"`
	Public  bool     `yaml:"public,omitempty" json:"public,omitempty"`
	Regions []string `yaml:"regions,omitempty" json:"regions,omitempty"`
}

// Azure describes upload options for Azure images.
//   Enabled: upload if true
//   ResourceGroup: the name of the Azure resource group
//   StorageAccount: name of the storage account
//   StorageContainer: name of the storage container
//   StorageLocation: name of the Azure region, i.e. us-east-1
type Azure struct {
	Enabled          bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ResourceGroup    string `yaml:"resource_group,omitempty" json:"resource_group,omitempty"`
	StorageAccount   string `yaml:"storage_account,omitempty" json:"stoarge_account,omitempty"`
	StorageContainer string `yaml:"storage_container,omitempty" json:"storage_container,omitempty"`
	StorageLocation  string `yaml:"storage_location,omitempty" json:"storage_location,omitempty"`
}

// Brew is the RHEL Koji instance for storing artifacts.
// 	 Principle: the Kerberos user
//   Profile: the profile to use, i.e. brew-testing
//   Tag: the Brew tag to tag the build as.
type Brew struct {
	Enabled   bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Principle string `yaml:"principle,omitempty" json:"principle,omitempty"`
	Profile   string `yaml:"profile,omitempty" json:"profile,omitempty"`
	Tag       string `yaml:"tag,omitempty" json:"tag,omitempty"`
}

// CloudsCfgs (yes Clouds) is a nested struct of all
// supported cloudClonfigurations.
type CloudsCfgs struct {
	Aliyun Aliyun `yaml:"aliyun,omitempty" json:"aliyun,omitempty"`
	Aws    Aws    `yaml:"aws,omitempty" json:"aws,omitempty"`
	Azure  Azure  `yaml:"azure,omitempty" json:"azure,omitempty"`
	Gcp    Gcp    `yaml:"gcp,omitempty" json:"gcp,omitempty"`
}

// Gcp describes deploiying to the GCP environment
//   Bucket: name of GCP bucket to store image in
//   Enabled: when true, publish to GCP
//   Project: name of the GCP project to use
type Gcp struct {
	Bucket  string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Project string `yaml:"project,omitempty" json:"project,omitempty"`
}

// Job refers to the Jenkins options
//   BuildName: i.e. rhcos-4.7
//   IsProduction: enforce KOLA tests
//   StrictMode: only run explicitly defined stages
//   VersionSuffix: name to append, ie. devel
type Job struct {
	BuildName     string `yaml:"build_name,omitempty" json:"build_name,omitempty"`
	IsProduction  bool   `yaml:"is_production,omitempty" json:"is_production,omitempty"`
	StrictMode    bool   `yaml:"strict,omitempty" json:"strict,omitempty"`
	VersionSuffix string `yaml:"version_suffix,omitempty" json:"version_suffix,omitempty"`
}

// Recipe describes where to get the build recipe/config, i.e fedora-coreos-config
//   GitRef: branch/ref to fetch from
//   GitUrl: url of the repo
type Recipe struct {
	GitRef string  `yaml:"git_ref,omitempty" json:"git_ref,omitempty"`
	GitURL string  `yaml:"git_url,omitempty" json:"git_url,omitempty"`
	Repos  []*Repo `yaml:"repos,omitempty" json:"repos,omitempty"`
}

// Repo is a yum/dnf repositories to use as an installation source.
type Repo struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

// Writer places the remote repo file into path. If the repo has no name,
// then a SHA256 of the URL will be used. Returns path of the file and err.
func (r *Repo) Writer(path string) (string, error) {
	if r.URL == "" {
		return "", errors.New("URL is undefined")
	}
	rname := r.Name
	if rname == "" {
		h := sha256.New()
		if _, err := h.Write([]byte(r.URL)); err != nil {
			return "", fmt.Errorf("failed to calculate name: %v", err)
		}
		rname = fmt.Sprintf("%x", h.Sum(nil))
	}

	f := filepath.Join(path, fmt.Sprintf("%s.repo", rname))
	out, err := os.OpenFile(f, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return f, fmt.Errorf("failed to open %s for writing: %v", f, err)
	}
	defer out.Close()

	resp, err := http.Get(r.URL)
	if err != nil {
		return f, err
	}
	defer resp.Body.Close()

	n, err := io.Copy(out, resp.Body)
	if n == 0 {
		return f, errors.New("No remote content fetched")
	}
	return f, err
}

// S3 describes the location of the S3 Resource.
//   Acl: is the s3 acl to use, usually 'private' or 'public'
//   Bucket: name of the S3 bucket
//   Path: the path inside the bucket
type S3 struct {
	ACL    string `yaml:"acl,omitempty" envVar:"S3_ACL" json:"acl,omitempty"`
	Bucket string `yaml:"bucket,omitempty" envVar:"S3_BUCKET" json:"bucket,omitempty"`
	Path   string `yaml:"path,omitempty" envVar:"S3_PATH" json:"path,omitempty"`
}

// Spec describes the RHCOS JobSpec.
//   GitRef: branch/ref to fetch from
//   GitUrl: url of the repo
type Spec struct {
	GitRef string `yaml:"git_ref,omitempty" json:"git_ref,omitempty"`
	GitURL string `yaml:"git_url,omitempty" json:"git_url,omitempty"`
}

// Oscontainer describes the location to push the OS Container to.
type Oscontainer struct {
	PushURL string `yaml:"push_url,omitempty" json:"push_url,omitempty"`
}

// JobSpecReader takes and io.Reader and returns a ptr to the JobSpec and err
func JobSpecReader(in io.Reader) (j JobSpec, err error) {
	d, err := ioutil.ReadAll(in)
	if err != nil {
		return j, err
	}

	err = yaml.Unmarshal(d, &j)
	if err != nil {
		return j, err
	}
	return j, err
}

// JobSpecFromFile return a JobSpec read from a file
func JobSpecFromFile(f string) (j JobSpec, err error) {
	in, err := os.Open(f)
	if err != nil {
		return j, err
	}
	defer in.Close()
	b := bufio.NewReader(in)
	return JobSpecReader(b)
}

// WriteJSON returns the jobspec
func (js *JobSpec) WriteJSON(w io.Writer) error {
	encode := json.NewEncoder(w)
	encode.SetIndent("", "  ")
	return encode.Encode(*js)
}

// WriteYAML returns the jobspec in YAML
func (js *JobSpec) WriteYAML(w io.Writer) error {
	encode := yaml.NewEncoder(w)
	return encode.Encode(*js)
}
