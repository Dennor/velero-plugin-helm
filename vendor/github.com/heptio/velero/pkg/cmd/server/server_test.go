/*
Copyright 2017 the Heptio Ark contributors.

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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/heptio/velero/pkg/apis/velero/v1"
	velerotest "github.com/heptio/velero/pkg/util/test"
)

func TestVeleroResourcesExist(t *testing.T) {
	var (
		fakeDiscoveryHelper = &velerotest.FakeDiscoveryHelper{}
		server              = &server{
			logger:          velerotest.NewLogger(),
			discoveryHelper: fakeDiscoveryHelper,
		}
	)

	// Velero API group doesn't exist in discovery: should error
	fakeDiscoveryHelper.ResourceList = []*metav1.APIResourceList{
		{
			GroupVersion: "foo/v1",
			APIResources: []metav1.APIResource{
				{
					Name: "Backups",
					Kind: "Backup",
				},
			},
		},
	}
	assert.Error(t, server.veleroResourcesExist())

	// Velero API group doesn't contain any custom resources: should error
	veleroAPIResourceList := &metav1.APIResourceList{
		GroupVersion: v1.SchemeGroupVersion.String(),
	}

	fakeDiscoveryHelper.ResourceList = append(fakeDiscoveryHelper.ResourceList, veleroAPIResourceList)
	assert.Error(t, server.veleroResourcesExist())

	// Velero API group contains all custom resources: should not error
	for kind := range v1.CustomResources() {
		veleroAPIResourceList.APIResources = append(veleroAPIResourceList.APIResources, metav1.APIResource{
			Kind: kind,
		})
	}
	assert.NoError(t, server.veleroResourcesExist())

	// Velero API group contains some but not all custom resources: should error
	veleroAPIResourceList.APIResources = veleroAPIResourceList.APIResources[:3]
	assert.Error(t, server.veleroResourcesExist())
}
