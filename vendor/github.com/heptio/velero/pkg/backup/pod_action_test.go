/*
Copyright 2018 the Heptio Ark contributors.

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

package backup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/heptio/velero/pkg/kuberesource"
	velerotest "github.com/heptio/velero/pkg/util/test"
)

func TestPodActionAppliesTo(t *testing.T) {
	a := NewPodAction(velerotest.NewLogger())

	actual, err := a.AppliesTo()
	require.NoError(t, err)

	expected := ResourceSelector{
		IncludedResources: []string{"pods"},
	}
	assert.Equal(t, expected, actual)
}

func TestPodActionExecute(t *testing.T) {
	tests := []struct {
		name     string
		pod      runtime.Unstructured
		expected []ResourceIdentifier
	}{
		{
			name: "no spec.volumes",
			pod: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
					"namespace": "foo",
					"name": "bar"
				}
			}
			`),
		},
		{
			name: "persistentVolumeClaim without claimName",
			pod: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
					"namespace": "foo",
					"name": "bar"
				},
				"spec": {
					"volumes": [
						{
							"persistentVolumeClaim": {}
						}
					]
				}
			}
			`),
		},
		{
			name: "full test, mix of volume types",
			pod: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
					"namespace": "foo",
					"name": "bar"
				},
				"spec": {
					"volumes": [
						{
							"persistentVolumeClaim": {}
						},
						{
							"emptyDir": {}
						},
						{
							"persistentVolumeClaim": {"claimName": "claim1"}
						},
						{
							"emptyDir": {}
						},
						{
							"persistentVolumeClaim": {"claimName": "claim2"}
						}
					]
				}
			}
			`),
			expected: []ResourceIdentifier{
				{GroupResource: kuberesource.PersistentVolumeClaims, Namespace: "foo", Name: "claim1"},
				{GroupResource: kuberesource.PersistentVolumeClaims, Namespace: "foo", Name: "claim2"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := NewPodAction(velerotest.NewLogger())

			updated, additionalItems, err := a.Execute(test.pod, nil)
			require.NoError(t, err)
			assert.Equal(t, test.pod, updated)
			assert.Equal(t, test.expected, additionalItems)
		})
	}
}
