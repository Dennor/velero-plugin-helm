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

package output

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/cmd/util/downloadrequest"
	clientset "github.com/heptio/velero/pkg/generated/clientset/versioned"
)

func DescribeRestore(restore *v1.Restore, podVolumeRestores []v1.PodVolumeRestore, details bool, veleroClient clientset.Interface) string {
	return Describe(func(d *Describer) {
		d.DescribeMetadata(restore.ObjectMeta)

		d.Println()
		d.Printf("Backup:\t%s\n", restore.Spec.BackupName)

		d.Println()
		d.Printf("Namespaces:\n")
		var s string
		if len(restore.Spec.IncludedNamespaces) == 0 {
			s = "*"
		} else {
			s = strings.Join(restore.Spec.IncludedNamespaces, ", ")
		}
		d.Printf("\tIncluded:\t%s\n", s)
		if len(restore.Spec.ExcludedNamespaces) == 0 {
			s = "<none>"
		} else {
			s = strings.Join(restore.Spec.ExcludedNamespaces, ", ")
		}
		d.Printf("\tExcluded:\t%s\n", s)

		d.Println()
		d.Printf("Resources:\n")
		if len(restore.Spec.IncludedResources) == 0 {
			s = "*"
		} else {
			s = strings.Join(restore.Spec.IncludedResources, ", ")
		}
		d.Printf("\tIncluded:\t%s\n", s)
		if len(restore.Spec.ExcludedResources) == 0 {
			s = "<none>"
		} else {
			s = strings.Join(restore.Spec.ExcludedResources, ", ")
		}
		d.Printf("\tExcluded:\t%s\n", s)

		d.Printf("\tCluster-scoped:\t%s\n", BoolPointerString(restore.Spec.IncludeClusterResources, "excluded", "included", "auto"))

		d.Println()
		d.DescribeMap("Namespace mappings", restore.Spec.NamespaceMapping)

		d.Println()
		s = "<none>"
		if restore.Spec.LabelSelector != nil {
			s = metav1.FormatLabelSelector(restore.Spec.LabelSelector)
		}
		d.Printf("Label selector:\t%s\n", s)

		d.Println()
		d.Printf("Restore PVs:\t%s\n", BoolPointerString(restore.Spec.RestorePVs, "false", "true", "auto"))

		d.Println()
		d.Printf("Phase:\t%s\n", restore.Status.Phase)

		d.Println()
		d.Printf("Validation errors:")
		if len(restore.Status.ValidationErrors) == 0 {
			d.Printf("\t<none>\n")
		} else {
			for _, ve := range restore.Status.ValidationErrors {
				d.Printf("\t%s\n", ve)
			}
		}

		d.Println()
		describeRestoreResults(d, restore, veleroClient)

		if len(podVolumeRestores) > 0 {
			d.Println()
			describePodVolumeRestores(d, podVolumeRestores, details)
		}
	})
}

func describeRestoreResults(d *Describer, restore *v1.Restore, veleroClient clientset.Interface) {
	if restore.Status.Warnings == 0 && restore.Status.Errors == 0 {
		d.Printf("Warnings:\t<none>\nErrors:\t<none>\n")
		return
	}

	var buf bytes.Buffer
	var resultMap map[string]v1.RestoreResult

	if err := downloadrequest.Stream(veleroClient.VeleroV1(), restore.Namespace, restore.Name, v1.DownloadTargetKindRestoreResults, &buf, downloadRequestTimeout); err != nil {
		d.Printf("Warnings:\t<error getting warnings: %v>\n\nErrors:\t<error getting errors: %v>\n", err, err)
		return
	}

	if err := json.NewDecoder(&buf).Decode(&resultMap); err != nil {
		d.Printf("Warnings:\t<error decoding warnings: %v>\n\nErrors:\t<error decoding errors: %v>\n", err, err)
		return
	}

	describeRestoreResult(d, "Warnings", resultMap["warnings"])
	d.Println()
	describeRestoreResult(d, "Errors", resultMap["errors"])
}

func describeRestoreResult(d *Describer, name string, result v1.RestoreResult) {
	d.Printf("%s:\n", name)
	// TODO(1.0): only describe result.Velero
	d.DescribeSlice(1, "Velero", append(result.Ark, result.Velero...))
	d.DescribeSlice(1, "Cluster", result.Cluster)
	if len(result.Namespaces) == 0 {
		d.Printf("\tNamespaces: <none>\n")
	} else {
		d.Printf("\tNamespaces:\n")
		for ns, warnings := range result.Namespaces {
			d.DescribeSlice(2, ns, warnings)
		}
	}
}

// describePodVolumeRestores describes pod volume restores in human-readable format.
func describePodVolumeRestores(d *Describer, restores []v1.PodVolumeRestore, details bool) {
	if details {
		d.Printf("Restic Restores:\n")
	} else {
		d.Printf("Restic Restores (specify --details for more information):\n")
	}

	// separate restores by phase (combining <none> and New into a single group)
	restoresByPhase := groupRestoresByPhase(restores)

	// go through phases in a specific order
	for _, phase := range []string{
		string(v1.PodVolumeRestorePhaseCompleted),
		string(v1.PodVolumeRestorePhaseFailed),
		"In Progress",
		string(v1.PodVolumeRestorePhaseNew),
	} {
		if len(restoresByPhase[phase]) == 0 {
			continue
		}

		// if we're not printing details, just report the phase and count
		if !details {
			d.Printf("\t%s:\t%d\n", phase, len(restoresByPhase[phase]))
			continue
		}

		// group the restores in the current phase by pod (i.e. "ns/name")
		restoresByPod := new(volumesByPod)

		for _, restore := range restoresByPhase[phase] {
			restoresByPod.Add(restore.Spec.Pod.Namespace, restore.Spec.Pod.Name, restore.Spec.Volume)
		}

		d.Printf("\t%s:\n", phase)
		for _, restoreGroup := range restoresByPod.Sorted() {
			sort.Strings(restoreGroup.volumes)

			// print volumes restored up for this pod
			d.Printf("\t\t%s: %s\n", restoreGroup.label, strings.Join(restoreGroup.volumes, ", "))
		}
	}
}

func groupRestoresByPhase(restores []v1.PodVolumeRestore) map[string][]v1.PodVolumeRestore {
	restoresByPhase := make(map[string][]v1.PodVolumeRestore)

	phaseToGroup := map[v1.PodVolumeRestorePhase]string{
		v1.PodVolumeRestorePhaseCompleted:  string(v1.PodVolumeRestorePhaseCompleted),
		v1.PodVolumeRestorePhaseFailed:     string(v1.PodVolumeRestorePhaseFailed),
		v1.PodVolumeRestorePhaseInProgress: "In Progress",
		v1.PodVolumeRestorePhaseNew:        string(v1.PodVolumeRestorePhaseNew),
		"":                                 string(v1.PodVolumeRestorePhaseNew),
	}

	for _, restore := range restores {
		group := phaseToGroup[restore.Status.Phase]
		restoresByPhase[group] = append(restoresByPhase[group], restore)
	}

	return restoresByPhase
}
