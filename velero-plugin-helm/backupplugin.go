/*
Copyright 2017, 2019 the Velero contributors.

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
	"io"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	rspb "k8s.io/helm/pkg/proto/hapi/release"
	storage "k8s.io/helm/pkg/storage/driver"

	v1 "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/backup"
)

type storageFactory interface {
	Storage(namespace string) storage.Driver
	Name() string
}

type secretsStorage struct {
	kubeClient kubernetes.Interface
}

func (s *secretsStorage) Storage(namespace string) storage.Driver {
	secrets := s.kubeClient.CoreV1().
		Secrets(namespace)
	return storage.NewSecrets(secrets)
}

func (c *secretsStorage) Name() string { return "secrets" }

type configmapsStorage struct {
	kubeClient kubernetes.Interface
}

func (c *configmapsStorage) Storage(namespace string) storage.Driver {
	configmaps := c.kubeClient.CoreV1().
		ConfigMaps(namespace)
	return storage.NewConfigMaps(configmaps)
}

func (c *configmapsStorage) Name() string { return "configmaps" }

// BackupPlugin is a backup item action for helm chart.
type BackupPlugin struct {
	storage storageFactory
	log     logrus.FieldLogger
}

// AppliesTo returns configmaps/secrets that are deployed and owned by tiller.
func (p *BackupPlugin) AppliesTo() (backup.ResourceSelector, error) {
	return backup.ResourceSelector{
		IncludedResources: []string{p.storage.Name()},
		LabelSelector:     "OWNER=TILLER",
	}, nil
}

type manifest struct {
	ApiVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

func fromManifest(release *rspb.Release, manifestString string) ([]backup.ResourceIdentifier, error) {
	var resources []backup.ResourceIdentifier
	dec := yaml.NewDecoder(strings.NewReader(manifestString))
	for {
		var m manifest
		err := dec.Decode(&m)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		gv, err := schema.ParseGroupVersion(m.ApiVersion)
		if err != nil {
			return nil, err
		}
		resources = append(resources, backup.ResourceIdentifier{
			GroupResource: schema.GroupResource{
				Group:    gv.Group,
				Resource: m.Kind,
			},
			Namespace: release.GetNamespace(),
			Name:      m.Metadata.Name,
		})
	}
	return resources, nil
}

func filterReleaseName(releaseName string) func(rls *rspb.Release) bool {
	return func(rls *rspb.Release) bool {
		return rls.GetName() == releaseName
	}
}

func hookResources(release *rspb.Release, hook *rspb.Hook) ([]backup.ResourceIdentifier, error) {
	// Hook never ran, skip it
	if hook.GetLastRun().GetSeconds() == 0 {
		return nil, nil
	}
	for _, p := range hook.GetDeletePolicies() {
		// TODO: If hook has any other delete policies
		// aside from before-hook-creation we need to check
		// with kubernetes if it actually still exists, for now
		// hooks with delete policy other than before-hook-creation
		// will be skipped
		if p != rspb.Hook_BEFORE_HOOK_CREATION {
			return nil, nil
		}
	}
	return fromManifest(release, hook.GetManifest())
}

func (p *BackupPlugin) getIdentifiers(metadata metav1.Object) ([]backup.ResourceIdentifier, error) {
	driver := p.storage.Storage(metadata.GetNamespace())
	release, err := driver.Get(metadata.GetName())
	if err != nil {
		return nil, err
	}
	releaseVersions, err := driver.List(filterReleaseName(release.GetName()))
	if err != nil {
		return nil, err
	}
	resources := make([]backup.ResourceIdentifier, 0)
	// Backup all release revisions
	for _, relVer := range releaseVersions {
		// Only backup resources for releases that are deployed
		if relVer.GetInfo().GetStatus().GetCode() == rspb.Status_DEPLOYED {
			for _, hook := range relVer.GetHooks() {
				hookResources, err := hookResources(relVer, hook)
				if err != nil {
					return nil, err
				}
				resources = append(resources, hookResources...)
			}
			releaseResources, err := fromManifest(relVer, relVer.GetManifest())
			if err != nil {
				return nil, err
			}
			resources = append(resources, releaseResources...)
		}
		resources = append(resources, backup.ResourceIdentifier{
			GroupResource: schema.GroupResource{
				Resource: driver.Name(),
			},
			Namespace: metadata.GetNamespace(),
			Name:      relVer.GetName() + "." + "v" + strconv.FormatInt(int64(relVer.GetVersion()), 10),
		})
	}

	resourcesLog := make([]logrus.Fields, 0)
	for _, res := range resources {
		resourcesLog = append(resourcesLog, logrus.Fields{
			"group":     res.Group,
			"resource":  res.Resource,
			"name":      res.Name,
			"namespace": res.Namespace,
		})
	}
	fields := logrus.Fields{
		"release":   release.GetName(),
		"resources": resourcesLog,
	}
	p.log.WithFields(fields).Debug("backing up resources for helm release")
	return resources, nil
}

// Execute returns chart configmap/secret allong with all additional resources defined by chart.
func (p *BackupPlugin) Execute(item runtime.Unstructured, backup *v1.Backup) (runtime.Unstructured, []backup.ResourceIdentifier, error) {
	metadata, err := meta.Accessor(item)
	if err != nil {
		return nil, nil, err
	}
	identifiers, err := p.getIdentifiers(metadata)
	return item, identifiers, err
}
