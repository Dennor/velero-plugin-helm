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

package restic

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"

	velerov1api "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/cloudprovider/azure"
	velerov1listers "github.com/heptio/velero/pkg/generated/listers/velero/v1"
	"github.com/heptio/velero/pkg/util/filesystem"
)

const (
	DaemonSet                   = "restic"
	InitContainer               = "restic-wait"
	DefaultMaintenanceFrequency = 24 * time.Hour

	podAnnotationPrefix       = "snapshot.velero.io/"
	volumesToBackupAnnotation = "backup.velero.io/backup-volumes"

	// TODO(1.0) remove both legacy annotations
	podAnnotationLegacyPrefix       = "snapshot.ark.heptio.com/"
	volumesToBackupLegacyAnnotation = "backup.ark.heptio.com/backup-volumes"
)

// PodHasSnapshotAnnotation returns true if the object has an annotation
// indicating that there is a restic snapshot for a volume in this pod,
// or false otherwise.
func PodHasSnapshotAnnotation(obj metav1.Object) bool {
	for key := range obj.GetAnnotations() {
		if strings.HasPrefix(key, podAnnotationPrefix) {
			return true
		}

		// TODO(1.0): remove if statement & contents
		if strings.HasPrefix(key, podAnnotationLegacyPrefix) {
			return true
		}
	}

	return false
}

// GetPodSnapshotAnnotations returns a map, of volume name -> snapshot id,
// of all restic snapshots for this pod.
func GetPodSnapshotAnnotations(obj metav1.Object) map[string]string {
	var res map[string]string

	insertSafe := func(k, v string) {
		if res == nil {
			res = make(map[string]string)
		}
		res[k] = v
	}

	for k, v := range obj.GetAnnotations() {
		if strings.HasPrefix(k, podAnnotationPrefix) {
			insertSafe(k[len(podAnnotationPrefix):], v)
		}

		if strings.HasPrefix(k, podAnnotationLegacyPrefix) {
			volume := k[len(podAnnotationLegacyPrefix):]

			// if it has the legacy prefix, only use it if there's not
			// already a value in res for the volume
			if _, ok := res[volume]; !ok {
				insertSafe(volume, v)
			}
		}
	}

	return res
}

// SetPodSnapshotAnnotation adds an annotation to a pod to indicate that
// the specified volume has a restic snapshot with the provided id.
func SetPodSnapshotAnnotation(obj metav1.Object, volumeName, snapshotID string) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[podAnnotationPrefix+volumeName] = snapshotID

	obj.SetAnnotations(annotations)
}

// GetVolumesToBackup returns a list of volume names to backup for
// the provided pod.
func GetVolumesToBackup(obj metav1.Object) []string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	backupsValue, ok := annotations[volumesToBackupAnnotation]
	// TODO(1.0) remove the following if statement & contents
	if !ok {
		backupsValue = annotations[volumesToBackupLegacyAnnotation]
	}

	if backupsValue == "" {
		return nil
	}

	return strings.Split(backupsValue, ",")
}

// SnapshotIdentifier uniquely identifies a restic snapshot
// taken by Velero.
type SnapshotIdentifier struct {
	// VolumeNamespace is the namespace of the pod/volume that
	// the restic snapshot is for.
	VolumeNamespace string

	// BackupStorageLocation is the backup's storage location
	// name.
	BackupStorageLocation string

	// SnapshotID is the short ID of the restic snapshot.
	SnapshotID string
}

// GetSnapshotsInBackup returns a list of all restic snapshot ids associated with
// a given Velero backup.
func GetSnapshotsInBackup(backup *velerov1api.Backup, podVolumeBackupLister velerov1listers.PodVolumeBackupLister) ([]SnapshotIdentifier, error) {
	selector := labels.Set(map[string]string{
		velerov1api.BackupNameLabel: backup.Name,
	}).AsSelector()

	podVolumeBackups, err := podVolumeBackupLister.List(selector)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var res []SnapshotIdentifier
	for _, item := range podVolumeBackups {
		if item.Status.SnapshotID == "" {
			continue
		}
		res = append(res, SnapshotIdentifier{
			VolumeNamespace:       item.Spec.Pod.Namespace,
			BackupStorageLocation: backup.Spec.StorageLocation,
			SnapshotID:            item.Status.SnapshotID,
		})
	}

	return res, nil
}

// TempCredentialsFile creates a temp file containing a restic
// encryption key for the given repo and returns its path. The
// caller should generally call os.Remove() to remove the file
// when done with it.
func TempCredentialsFile(secretLister corev1listers.SecretLister, veleroNamespace, repoName string, fs filesystem.Interface) (string, error) {
	secretGetter := NewListerSecretGetter(secretLister)

	// For now, all restic repos share the same key so we don't need the repoName to fetch it.
	// When we move to full-backup encryption, we'll likely have a separate key per restic repo
	// (all within the Velero server's namespace) so GetRepositoryKey will need to take the repo
	// name as an argument as well.
	repoKey, err := GetRepositoryKey(secretGetter, veleroNamespace)
	if err != nil {
		return "", err
	}

	file, err := fs.TempFile("", fmt.Sprintf("%s-%s", CredentialsSecretName, repoName))
	if err != nil {
		return "", errors.WithStack(err)
	}

	if _, err := file.Write(repoKey); err != nil {
		// nothing we can do about an error closing the file here, and we're
		// already returning an error about the write failing.
		file.Close()
		return "", errors.WithStack(err)
	}

	name := file.Name()

	if err := file.Close(); err != nil {
		return "", errors.WithStack(err)
	}

	return name, nil
}

// NewPodVolumeBackupListOptions creates a ListOptions with a label selector configured to
// find PodVolumeBackups for the backup identified by name.
func NewPodVolumeBackupListOptions(name string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", velerov1api.BackupNameLabel, name),
	}
}

// NewPodVolumeRestoreListOptions creates a ListOptions with a label selector configured to
// find PodVolumeRestores for the restore identified by name.
func NewPodVolumeRestoreListOptions(name string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", velerov1api.RestoreNameLabel, name),
	}
}

// AzureCmdEnv returns a list of environment variables (in the format var=val) that
// should be used when running a restic command for an Azure backend. This list is
// the current environment, plus the Azure-specific variables restic needs, namely
// a storage account name and key.
func AzureCmdEnv(backupLocationLister velerov1listers.BackupStorageLocationLister, namespace, backupLocation string) ([]string, error) {
	loc, err := backupLocationLister.BackupStorageLocations(namespace).Get(backupLocation)
	if err != nil {
		return nil, errors.Wrap(err, "error getting backup storage location")
	}

	azureVars, err := azure.GetResticEnvVars(loc.Spec.Config)
	if err != nil {
		return nil, errors.Wrap(err, "error getting azure restic env vars")
	}

	env := os.Environ()
	for k, v := range azureVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env, nil
}
