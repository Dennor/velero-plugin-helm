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
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbac "k8s.io/api/rbac/v1"
	rbacbeta "k8s.io/api/rbac/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/heptio/velero/pkg/kuberesource"
	velerotest "github.com/heptio/velero/pkg/util/test"
)

func newV1ClusterRoleBindingList(rbacCRBList []rbac.ClusterRoleBinding) []ClusterRoleBinding {
	var crbs []ClusterRoleBinding
	for _, c := range rbacCRBList {
		crbs = append(crbs, v1ClusterRoleBinding{crb: c})
	}

	return crbs
}

func newV1beta1ClusterRoleBindingList(rbacCRBList []rbacbeta.ClusterRoleBinding) []ClusterRoleBinding {
	var crbs []ClusterRoleBinding
	for _, c := range rbacCRBList {
		crbs = append(crbs, v1beta1ClusterRoleBinding{crb: c})
	}

	return crbs
}

type FakeV1ClusterRoleBindingLister struct {
	v1crbs []rbac.ClusterRoleBinding
}

func (f FakeV1ClusterRoleBindingLister) List() ([]ClusterRoleBinding, error) {
	var crbs []ClusterRoleBinding
	for _, c := range f.v1crbs {
		crbs = append(crbs, v1ClusterRoleBinding{crb: c})
	}
	return crbs, nil
}

type FakeV1beta1ClusterRoleBindingLister struct {
	v1beta1crbs []rbacbeta.ClusterRoleBinding
}

func (f FakeV1beta1ClusterRoleBindingLister) List() ([]ClusterRoleBinding, error) {
	var crbs []ClusterRoleBinding
	for _, c := range f.v1beta1crbs {
		crbs = append(crbs, v1beta1ClusterRoleBinding{crb: c})
	}
	return crbs, nil
}

func TestServiceAccountActionAppliesTo(t *testing.T) {
	// Instantiating the struct directly since using
	// NewServiceAccountAction requires a full kubernetes clientset
	a := &serviceAccountAction{}

	actual, err := a.AppliesTo()
	require.NoError(t, err)

	expected := ResourceSelector{
		IncludedResources: []string{"serviceaccounts"},
	}
	assert.Equal(t, expected, actual)
}

func TestNewServiceAccountAction(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		expectedCRBs []ClusterRoleBinding
	}{
		{
			name:    "rbac v1 API instantiates an saAction",
			version: rbac.SchemeGroupVersion.Version,
			expectedCRBs: []ClusterRoleBinding{
				v1ClusterRoleBinding{
					crb: rbac.ClusterRoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "v1crb-1",
						},
					},
				},
				v1ClusterRoleBinding{
					crb: rbac.ClusterRoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "v1crb-2",
						},
					},
				},
			},
		},
		{
			name:    "rbac v1beta1 API instantiates an saAction",
			version: rbacbeta.SchemeGroupVersion.Version,
			expectedCRBs: []ClusterRoleBinding{
				v1beta1ClusterRoleBinding{
					crb: rbacbeta.ClusterRoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "v1beta1crb-1",
						},
					},
				},
				v1beta1ClusterRoleBinding{
					crb: rbacbeta.ClusterRoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "v1beta1crb-2",
						},
					},
				},
			},
		},
		{
			name:         "no RBAC API instantiates an saAction with empty slice",
			version:      "",
			expectedCRBs: []ClusterRoleBinding{},
		},
	}
	// Set up all of our fakes outside the test loop
	discoveryHelper := velerotest.FakeDiscoveryHelper{}
	logger := velerotest.NewLogger()

	v1crbs := []rbac.ClusterRoleBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "v1crb-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "v1crb-2",
			},
		},
	}

	v1beta1crbs := []rbacbeta.ClusterRoleBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "v1beta1crb-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "v1beta1crb-2",
			},
		},
	}

	clusterRoleBindingListers := map[string]ClusterRoleBindingLister{
		rbac.SchemeGroupVersion.Version:     FakeV1ClusterRoleBindingLister{v1crbs: v1crbs},
		rbacbeta.SchemeGroupVersion.Version: FakeV1beta1ClusterRoleBindingLister{v1beta1crbs: v1beta1crbs},
		"":                                  noopClusterRoleBindingLister{},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// We only care about the preferred version, nothing else in the list
			discoveryHelper.APIGroupsList = []metav1.APIGroup{
				{
					Name: rbac.GroupName,
					PreferredVersion: metav1.GroupVersionForDiscovery{
						Version: test.version,
					},
				},
			}
			action, err := NewServiceAccountAction(logger, clusterRoleBindingListers, &discoveryHelper)
			require.NoError(t, err)
			saAction, ok := action.(*serviceAccountAction)
			require.True(t, ok)
			assert.Equal(t, test.expectedCRBs, saAction.clusterRoleBindings)
		})
	}
}

func TestServiceAccountActionExecute(t *testing.T) {
	tests := []struct {
		name                    string
		serviceAccount          runtime.Unstructured
		crbs                    []rbac.ClusterRoleBinding
		expectedAdditionalItems []ResourceIdentifier
	}{
		{
			name: "no crbs",
			serviceAccount: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "ServiceAccount",
				"metadata": {
					"namespace": "velero",
					"name": "velero"
				}
			}
			`),
			crbs:                    nil,
			expectedAdditionalItems: nil,
		},
		{
			name: "no matching crbs",
			serviceAccount: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "ServiceAccount",
				"metadata": {
					"namespace": "velero",
					"name": "velero"
				}
			}
			`),
			crbs: []rbac.ClusterRoleBinding{
				{
					Subjects: []rbac.Subject{
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
						{
							Kind:      "non-matching-kind",
							Namespace: "velero",
							Name:      "velero",
						},
						{
							Kind:      rbac.ServiceAccountKind,
							Namespace: "non-matching-ns",
							Name:      "velero",
						},
						{
							Kind:      rbac.ServiceAccountKind,
							Namespace: "velero",
							Name:      "non-matching-name",
						},
					},
					RoleRef: rbac.RoleRef{
						Name: "role",
					},
				},
			},
			expectedAdditionalItems: nil,
		},
		{
			name: "some matching crbs",
			serviceAccount: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "ServiceAccount",
				"metadata": {
					"namespace": "velero",
					"name": "velero"
				}
			}
			`),
			crbs: []rbac.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-1",
					},
					Subjects: []rbac.Subject{
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
					},
					RoleRef: rbac.RoleRef{
						Name: "role-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-2",
					},
					Subjects: []rbac.Subject{
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
						{
							Kind:      rbac.ServiceAccountKind,
							Namespace: "velero",
							Name:      "velero",
						},
					},
					RoleRef: rbac.RoleRef{
						Name: "role-2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-3",
					},
					Subjects: []rbac.Subject{
						{
							Kind:      rbac.ServiceAccountKind,
							Namespace: "velero",
							Name:      "velero",
						},
					},
					RoleRef: rbac.RoleRef{
						Name: "role-3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-4",
					},
					Subjects: []rbac.Subject{
						{
							Kind:      rbac.ServiceAccountKind,
							Namespace: "velero",
							Name:      "velero",
						},
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
					},
					RoleRef: rbac.RoleRef{
						Name: "role-4",
					},
				},
			},
			expectedAdditionalItems: []ResourceIdentifier{
				{
					GroupResource: kuberesource.ClusterRoleBindings,
					Name:          "crb-2",
				},
				{
					GroupResource: kuberesource.ClusterRoleBindings,
					Name:          "crb-3",
				},
				{
					GroupResource: kuberesource.ClusterRoleBindings,
					Name:          "crb-4",
				},
				{
					GroupResource: kuberesource.ClusterRoles,
					Name:          "role-2",
				},
				{
					GroupResource: kuberesource.ClusterRoles,
					Name:          "role-3",
				},
				{
					GroupResource: kuberesource.ClusterRoles,
					Name:          "role-4",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create the action struct directly so we don't need to mock a clientset
			action := &serviceAccountAction{
				log:                 velerotest.NewLogger(),
				clusterRoleBindings: newV1ClusterRoleBindingList(test.crbs),
			}

			res, additional, err := action.Execute(test.serviceAccount, nil)

			assert.Equal(t, test.serviceAccount, res)
			assert.Nil(t, err)

			// ensure slices are ordered for valid comparison
			sort.Slice(test.expectedAdditionalItems, func(i, j int) bool {
				return fmt.Sprintf("%s.%s", test.expectedAdditionalItems[i].GroupResource.String(), test.expectedAdditionalItems[i].Name) <
					fmt.Sprintf("%s.%s", test.expectedAdditionalItems[j].GroupResource.String(), test.expectedAdditionalItems[j].Name)
			})

			sort.Slice(additional, func(i, j int) bool {
				return fmt.Sprintf("%s.%s", additional[i].GroupResource.String(), additional[i].Name) <
					fmt.Sprintf("%s.%s", additional[j].GroupResource.String(), additional[j].Name)
			})

			assert.Equal(t, test.expectedAdditionalItems, additional)
		})
	}

}

func TestServiceAccountActionExecuteOnBeta1(t *testing.T) {
	tests := []struct {
		name                    string
		serviceAccount          runtime.Unstructured
		crbs                    []rbacbeta.ClusterRoleBinding
		expectedAdditionalItems []ResourceIdentifier
	}{
		{
			name: "no crbs",
			serviceAccount: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "ServiceAccount",
				"metadata": {
					"namespace": "velero",
					"name": "velero"
				}
			}
			`),
			crbs:                    nil,
			expectedAdditionalItems: nil,
		},
		{
			name: "no matching crbs",
			serviceAccount: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "ServiceAccount",
				"metadata": {
					"namespace": "velero",
					"name": "velero"
				}
			}
			`),
			crbs: []rbacbeta.ClusterRoleBinding{
				{
					Subjects: []rbacbeta.Subject{
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
						{
							Kind:      "non-matching-kind",
							Namespace: "velero",
							Name:      "velero",
						},
						{
							Kind:      rbacbeta.ServiceAccountKind,
							Namespace: "non-matching-ns",
							Name:      "velero",
						},
						{
							Kind:      rbacbeta.ServiceAccountKind,
							Namespace: "velero",
							Name:      "non-matching-name",
						},
					},
					RoleRef: rbacbeta.RoleRef{
						Name: "role",
					},
				},
			},
			expectedAdditionalItems: nil,
		},
		{
			name: "some matching crbs",
			serviceAccount: velerotest.UnstructuredOrDie(`
			{
				"apiVersion": "v1",
				"kind": "ServiceAccount",
				"metadata": {
					"namespace": "velero",
					"name": "velero"
				}
			}
			`),
			crbs: []rbacbeta.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-1",
					},
					Subjects: []rbacbeta.Subject{
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
					},
					RoleRef: rbacbeta.RoleRef{
						Name: "role-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-2",
					},
					Subjects: []rbacbeta.Subject{
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
						{
							Kind:      rbacbeta.ServiceAccountKind,
							Namespace: "velero",
							Name:      "velero",
						},
					},
					RoleRef: rbacbeta.RoleRef{
						Name: "role-2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-3",
					},
					Subjects: []rbacbeta.Subject{
						{
							Kind:      rbacbeta.ServiceAccountKind,
							Namespace: "velero",
							Name:      "velero",
						},
					},
					RoleRef: rbacbeta.RoleRef{
						Name: "role-3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-4",
					},
					Subjects: []rbacbeta.Subject{
						{
							Kind:      rbacbeta.ServiceAccountKind,
							Namespace: "velero",
							Name:      "velero",
						},
						{
							Kind:      "non-matching-kind",
							Namespace: "non-matching-ns",
							Name:      "non-matching-name",
						},
					},
					RoleRef: rbacbeta.RoleRef{
						Name: "role-4",
					},
				},
			},
			expectedAdditionalItems: []ResourceIdentifier{
				{
					GroupResource: kuberesource.ClusterRoleBindings,
					Name:          "crb-2",
				},
				{
					GroupResource: kuberesource.ClusterRoleBindings,
					Name:          "crb-3",
				},
				{
					GroupResource: kuberesource.ClusterRoleBindings,
					Name:          "crb-4",
				},
				{
					GroupResource: kuberesource.ClusterRoles,
					Name:          "role-2",
				},
				{
					GroupResource: kuberesource.ClusterRoles,
					Name:          "role-3",
				},
				{
					GroupResource: kuberesource.ClusterRoles,
					Name:          "role-4",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create the action struct directly so we don't need to mock a clientset
			action := &serviceAccountAction{
				log:                 velerotest.NewLogger(),
				clusterRoleBindings: newV1beta1ClusterRoleBindingList(test.crbs),
			}

			res, additional, err := action.Execute(test.serviceAccount, nil)

			assert.Equal(t, test.serviceAccount, res)
			assert.Nil(t, err)

			// ensure slices are ordered for valid comparison
			sort.Slice(test.expectedAdditionalItems, func(i, j int) bool {
				return fmt.Sprintf("%s.%s", test.expectedAdditionalItems[i].GroupResource.String(), test.expectedAdditionalItems[i].Name) <
					fmt.Sprintf("%s.%s", test.expectedAdditionalItems[j].GroupResource.String(), test.expectedAdditionalItems[j].Name)
			})

			sort.Slice(additional, func(i, j int) bool {
				return fmt.Sprintf("%s.%s", additional[i].GroupResource.String(), additional[i].Name) <
					fmt.Sprintf("%s.%s", additional[j].GroupResource.String(), additional[j].Name)
			})

			assert.Equal(t, test.expectedAdditionalItems, additional)
		})
	}

}
