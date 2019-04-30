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
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	clientset "github.com/heptio/velero/pkg/generated/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	rspb "k8s.io/helm/pkg/proto/hapi/release"
	storage "k8s.io/helm/pkg/storage/driver"

	v1 "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/backup"
	kcmdutil "github.com/heptio/velero/third_party/kubernetes/pkg/kubectl/cmd/util"
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
	clientset clientset.Interface
	log       logrus.FieldLogger
	storage   storageFactory
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

func (r *releaseBackup) resourceNamespace(apiResource *metav1.APIResource) string {
	if apiResource.Namespaced {
		return r.release.GetNamespace()
	}
	return ""
}

func (r *releaseBackup) fromManifest(manifestString string) ([]backup.ResourceIdentifier, error) {
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
		gvr, apiResource, err := r.ResourceFor(gv.WithKind(m.Kind))
		if err != nil {
			return nil, err
		}

		resources = append(resources, backup.ResourceIdentifier{
			GroupResource: schema.GroupResource{
				Group:    gvr.Group,
				Resource: gvr.Resource,
			},
			Namespace: r.resourceNamespace(&apiResource),
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

func (r *releaseBackup) hookResources(hook *rspb.Hook) ([]backup.ResourceIdentifier, error) {
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
	return r.fromManifest(hook.GetManifest())
}

func (r *releaseBackup) ResourceFor(gvk schema.GroupVersionKind) (schema.GroupVersionResource, metav1.APIResource, error) {
	if resource, ok := r.resourcesMap[gvk]; ok {
		return schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: resource.Name,
		}, resource, nil
	}
	m, err := r.mapper.RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, metav1.APIResource{}, err
	}
	if resource, ok := r.resourcesMap[m.GroupVersionKind]; ok {
		return schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: resource.Name,
		}, resource, nil
	}
	return schema.GroupVersionResource{}, metav1.APIResource{}, errors.WithStack(fmt.Errorf("APIResource for %v not found", gvk))
}

type releaseBackup struct {
	metadata     metav1.Object
	log          logrus.FieldLogger
	driver       storage.Driver
	resourcesMap map[schema.GroupVersionKind]metav1.APIResource
	mapper       meta.RESTMapper
	resources    []*metav1.APIResourceList
	release      *rspb.Release
}

func (r *releaseBackup) runReleaseBackup() ([]backup.ResourceIdentifier, error) {
	release, err := r.driver.Get(r.metadata.GetName())
	if err != nil {
		return nil, err
	}
	r.release = release
	releaseVersions, err := r.driver.List(filterReleaseName(release.GetName()))
	if err != nil {
		return nil, err
	}
	resources := make([]backup.ResourceIdentifier, 0)
	// Backup all release revisions
	for _, relVer := range releaseVersions {
		// Only backup resources for releases that are deployed
		if relVer.GetInfo().GetStatus().GetCode() == rspb.Status_DEPLOYED {
			for _, hook := range relVer.GetHooks() {
				hookResources, err := r.hookResources(hook)
				if err != nil {
					return nil, err
				}
				resources = append(resources, hookResources...)
			}
			releaseResources, err := r.fromManifest(relVer.GetManifest())
			if err != nil {
				return nil, err
			}
			resources = append(resources, releaseResources...)
		}
		resources = append(resources, backup.ResourceIdentifier{
			GroupResource: schema.GroupResource{
				Resource: r.driver.Name(),
			},
			Namespace: r.metadata.GetNamespace(),
			Name:      relVer.GetName() + "." + "v" + strconv.FormatInt(int64(relVer.GetVersion()), 10),
		})
	}

	return resources, nil
}

// Source: https://github.com/heptio/velero/blob/master/pkg/discovery/helper.go
func filterByVerbs(groupVersion string, r *metav1.APIResource) bool {
	return discovery.SupportsAllVerbs{Verbs: []string{"list", "create", "get", "delete"}}.Match(groupVersion, r)
}

func refreshServerPreferredResources(discoveryClient discovery.DiscoveryInterface, logger logrus.FieldLogger) ([]*metav1.APIResourceList, error) {
	preferredResources, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		if discoveryErr, ok := err.(*discovery.ErrGroupDiscoveryFailed); ok {
			for groupVersion, err := range discoveryErr.Groups {
				logger.WithError(err).Warnf("Failed to discover group: %v", groupVersion)
			}
			return preferredResources, nil
		}
	}
	return preferredResources, err
}

func (p *BackupPlugin) getIdentifiers(metadata metav1.Object) ([]backup.ResourceIdentifier, error) {
	driver := p.storage.Storage(metadata.GetNamespace())
	discoveryClient := p.clientset.Discovery()
	releaseBackup := releaseBackup{
		metadata:     metadata,
		driver:       driver,
		log:          p.log,
		resourcesMap: make(map[schema.GroupVersionKind]metav1.APIResource),
	}
	groupResources, err := restmapper.GetAPIGroupResources(p.clientset.Discovery())
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	shortcutExpander, err := kcmdutil.NewShortcutExpander(mapper, discoveryClient, p.log)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	releaseBackup.mapper = shortcutExpander
	preferredResources, err := refreshServerPreferredResources(discoveryClient, p.log)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	releaseBackup.resources = discovery.FilteredBy(
		discovery.ResourcePredicateFunc(filterByVerbs),
		preferredResources,
	)
	for _, resourceGroup := range releaseBackup.resources {
		gv, err := schema.ParseGroupVersion(resourceGroup.GroupVersion)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to parse GroupVersion %s", resourceGroup.GroupVersion)
		}
		for _, resource := range resourceGroup.APIResources {
			gvk := gv.WithKind(resource.Kind)
			releaseBackup.resourcesMap[gvk] = resource
		}
	}
	return releaseBackup.runReleaseBackup()
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
