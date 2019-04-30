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

package controller

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"

	velerov1api "github.com/heptio/velero/pkg/apis/velero/v1"
	pkgbackup "github.com/heptio/velero/pkg/backup"
	velerov1client "github.com/heptio/velero/pkg/generated/clientset/versioned/typed/velero/v1"
	informers "github.com/heptio/velero/pkg/generated/informers/externalversions/velero/v1"
	listers "github.com/heptio/velero/pkg/generated/listers/velero/v1"
	"github.com/heptio/velero/pkg/metrics"
	"github.com/heptio/velero/pkg/persistence"
	"github.com/heptio/velero/pkg/plugin"
	"github.com/heptio/velero/pkg/util/collections"
	"github.com/heptio/velero/pkg/util/encode"
	kubeutil "github.com/heptio/velero/pkg/util/kube"
	"github.com/heptio/velero/pkg/util/logging"
	"github.com/heptio/velero/pkg/volume"
)

type backupController struct {
	*genericController

	backupper                pkgbackup.Backupper
	lister                   listers.BackupLister
	client                   velerov1client.BackupsGetter
	clock                    clock.Clock
	backupLogLevel           logrus.Level
	newPluginManager         func(logrus.FieldLogger) plugin.Manager
	backupTracker            BackupTracker
	backupLocationLister     listers.BackupStorageLocationLister
	defaultBackupLocation    string
	snapshotLocationLister   listers.VolumeSnapshotLocationLister
	defaultSnapshotLocations map[string]string
	metrics                  *metrics.ServerMetrics
	newBackupStore           func(*velerov1api.BackupStorageLocation, persistence.ObjectStoreGetter, logrus.FieldLogger) (persistence.BackupStore, error)
}

func NewBackupController(
	backupInformer informers.BackupInformer,
	client velerov1client.BackupsGetter,
	backupper pkgbackup.Backupper,
	logger logrus.FieldLogger,
	backupLogLevel logrus.Level,
	newPluginManager func(logrus.FieldLogger) plugin.Manager,
	backupTracker BackupTracker,
	backupLocationInformer informers.BackupStorageLocationInformer,
	defaultBackupLocation string,
	volumeSnapshotLocationInformer informers.VolumeSnapshotLocationInformer,
	defaultSnapshotLocations map[string]string,
	metrics *metrics.ServerMetrics,
) Interface {
	c := &backupController{
		genericController:        newGenericController("backup", logger),
		backupper:                backupper,
		lister:                   backupInformer.Lister(),
		client:                   client,
		clock:                    &clock.RealClock{},
		backupLogLevel:           backupLogLevel,
		newPluginManager:         newPluginManager,
		backupTracker:            backupTracker,
		backupLocationLister:     backupLocationInformer.Lister(),
		defaultBackupLocation:    defaultBackupLocation,
		snapshotLocationLister:   volumeSnapshotLocationInformer.Lister(),
		defaultSnapshotLocations: defaultSnapshotLocations,
		metrics:                  metrics,

		newBackupStore: persistence.NewObjectBackupStore,
	}

	c.syncHandler = c.processBackup
	c.cacheSyncWaiters = append(c.cacheSyncWaiters,
		backupInformer.Informer().HasSynced,
		backupLocationInformer.Informer().HasSynced,
		volumeSnapshotLocationInformer.Informer().HasSynced,
	)

	backupInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				backup := obj.(*velerov1api.Backup)

				switch backup.Status.Phase {
				case "", velerov1api.BackupPhaseNew:
					// only process new backups
				default:
					c.logger.WithFields(logrus.Fields{
						"backup": kubeutil.NamespaceAndName(backup),
						"phase":  backup.Status.Phase,
					}).Debug("Backup is not new, skipping")
					return
				}

				key, err := cache.MetaNamespaceKeyFunc(backup)
				if err != nil {
					c.logger.WithError(err).WithField("backup", backup).Error("Error creating queue key, item not added to queue")
					return
				}
				c.queue.Add(key)
			},
		},
	)

	return c
}

func (c *backupController) processBackup(key string) error {
	log := c.logger.WithField("key", key)

	log.Debug("Running processBackup")
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return errors.Wrap(err, "error splitting queue key")
	}

	log.Debug("Getting backup")
	original, err := c.lister.Backups(ns).Get(name)
	if err != nil {
		return errors.Wrap(err, "error getting backup")
	}

	// Double-check we have the correct phase. In the unlikely event that multiple controller
	// instances are running, it's possible for controller A to succeed in changing the phase to
	// InProgress, while controller B's attempt to patch the phase fails. When controller B
	// reprocesses the same backup, it will either show up as New (informer hasn't seen the update
	// yet) or as InProgress. In the former case, the patch attempt will fail again, until the
	// informer sees the update. In the latter case, after the informer has seen the update to
	// InProgress, we still need this check so we can return nil to indicate we've finished processing
	// this key (even though it was a no-op).
	switch original.Status.Phase {
	case "", velerov1api.BackupPhaseNew:
		// only process new backups
	default:
		return nil
	}

	log.Debug("Preparing backup request")
	request := c.prepareBackupRequest(original)

	if len(request.Status.ValidationErrors) > 0 {
		request.Status.Phase = velerov1api.BackupPhaseFailedValidation
	} else {
		request.Status.Phase = velerov1api.BackupPhaseInProgress
	}

	// update status
	updatedBackup, err := patchBackup(original, request.Backup, c.client)
	if err != nil {
		return errors.Wrapf(err, "error updating Backup status to %s", request.Status.Phase)
	}
	// store ref to just-updated item for creating patch
	original = updatedBackup
	request.Backup = updatedBackup.DeepCopy()

	if request.Status.Phase == velerov1api.BackupPhaseFailedValidation {
		return nil
	}

	c.backupTracker.Add(request.Namespace, request.Name)
	defer c.backupTracker.Delete(request.Namespace, request.Name)

	log.Debug("Running backup")
	// execution & upload of backup
	backupScheduleName := request.GetLabels()[velerov1api.ScheduleNameLabel]
	c.metrics.RegisterBackupAttempt(backupScheduleName)

	if err := c.runBackup(request); err != nil {
		log.WithError(err).Error("backup failed")
		request.Status.Phase = velerov1api.BackupPhaseFailed
		c.metrics.RegisterBackupFailed(backupScheduleName)
	} else {
		c.metrics.RegisterBackupSuccess(backupScheduleName)
	}

	log.Debug("Updating backup's final status")
	if _, err := patchBackup(original, request.Backup, c.client); err != nil {
		log.WithError(err).Error("error updating backup's final status")
	}

	return nil
}

func patchBackup(original, updated *velerov1api.Backup, client velerov1client.BackupsGetter) (*velerov1api.Backup, error) {
	origBytes, err := json.Marshal(original)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(updated)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}

	res, err := client.Backups(original.Namespace).Patch(original.Name, types.MergePatchType, patchBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	return res, nil
}

func (c *backupController) prepareBackupRequest(backup *velerov1api.Backup) *pkgbackup.Request {
	request := &pkgbackup.Request{
		Backup: backup.DeepCopy(), // don't modify items in the cache
	}

	// set backup version
	request.Status.Version = pkgbackup.BackupVersion

	// calculate expiration
	if request.Spec.TTL.Duration > 0 {
		request.Status.Expiration = metav1.NewTime(c.clock.Now().Add(request.Spec.TTL.Duration))
	}

	// default storage location if not specified
	if request.Spec.StorageLocation == "" {
		request.Spec.StorageLocation = c.defaultBackupLocation
	}

	// add the storage location as a label for easy filtering later.
	if request.Labels == nil {
		request.Labels = make(map[string]string)
	}
	request.Labels[velerov1api.StorageLocationLabel] = request.Spec.StorageLocation

	// validate the included/excluded resources and namespaces
	for _, err := range collections.ValidateIncludesExcludes(request.Spec.IncludedResources, request.Spec.ExcludedResources) {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid included/excluded resource lists: %v", err))
	}

	for _, err := range collections.ValidateIncludesExcludes(request.Spec.IncludedNamespaces, request.Spec.ExcludedNamespaces) {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid included/excluded namespace lists: %v", err))
	}

	// validate the storage location, and store the BackupStorageLocation API obj on the request
	if storageLocation, err := c.backupLocationLister.BackupStorageLocations(request.Namespace).Get(request.Spec.StorageLocation); err != nil {
		if apierrors.IsNotFound(err) {
			request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("a BackupStorageLocation CRD with the name specified in the backup spec needs to be created before this backup can be executed. Error: %v", err))
		} else {
			request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("error getting backup storage location: %v", err))
		}
	} else {
		request.StorageLocation = storageLocation
	}

	// validate and get the backup's VolumeSnapshotLocations, and store the
	// VolumeSnapshotLocation API objs on the request
	if locs, errs := c.validateAndGetSnapshotLocations(request.Backup); len(errs) > 0 {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, errs...)
	} else {
		request.Spec.VolumeSnapshotLocations = nil
		for _, loc := range locs {
			request.Spec.VolumeSnapshotLocations = append(request.Spec.VolumeSnapshotLocations, loc.Name)
			request.SnapshotLocations = append(request.SnapshotLocations, loc)
		}
	}

	return request
}

// validateAndGetSnapshotLocations gets a collection of VolumeSnapshotLocation objects that
// this backup will use (returned as a map of provider name -> VSL), and ensures:
// - each location name in .spec.volumeSnapshotLocations exists as a location
// - exactly 1 location per provider
// - a given provider's default location name is added to .spec.volumeSnapshotLocations if one
//   is not explicitly specified for the provider (if there's only one location for the provider,
//   it will automatically be used)
func (c *backupController) validateAndGetSnapshotLocations(backup *velerov1api.Backup) (map[string]*velerov1api.VolumeSnapshotLocation, []string) {
	errors := []string{}
	providerLocations := make(map[string]*velerov1api.VolumeSnapshotLocation)

	for _, locationName := range backup.Spec.VolumeSnapshotLocations {
		// validate each locationName exists as a VolumeSnapshotLocation
		location, err := c.snapshotLocationLister.VolumeSnapshotLocations(backup.Namespace).Get(locationName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				errors = append(errors, fmt.Sprintf("a VolumeSnapshotLocation CRD for the location %s with the name specified in the backup spec needs to be created before this snapshot can be executed. Error: %v", locationName, err))
			} else {
				errors = append(errors, fmt.Sprintf("error getting volume snapshot location named %s: %v", locationName, err))
			}
			continue
		}

		// ensure we end up with exactly 1 location *per provider*
		if providerLocation, ok := providerLocations[location.Spec.Provider]; ok {
			// if > 1 location name per provider as in ["aws-us-east-1" | "aws-us-west-1"] (same provider, multiple names)
			if providerLocation.Name != locationName {
				errors = append(errors, fmt.Sprintf("more than one VolumeSnapshotLocation name specified for provider %s: %s; unexpected name was %s", location.Spec.Provider, locationName, providerLocation.Name))
				continue
			}
		} else {
			// keep track of all valid existing locations, per provider
			providerLocations[location.Spec.Provider] = location
		}
	}

	if len(errors) > 0 {
		return nil, errors
	}

	allLocations, err := c.snapshotLocationLister.VolumeSnapshotLocations(backup.Namespace).List(labels.Everything())
	if err != nil {
		errors = append(errors, fmt.Sprintf("error listing volume snapshot locations: %v", err))
		return nil, errors
	}

	// build a map of provider->list of all locations for the provider
	allProviderLocations := make(map[string][]*velerov1api.VolumeSnapshotLocation)
	for i := range allLocations {
		loc := allLocations[i]
		allProviderLocations[loc.Spec.Provider] = append(allProviderLocations[loc.Spec.Provider], loc)
	}

	// go through each provider and make sure we have/can get a VSL
	// for it
	for provider, locations := range allProviderLocations {
		if _, ok := providerLocations[provider]; ok {
			// backup's spec had a location named for this provider
			continue
		}

		if len(locations) > 1 {
			// more than one possible location for the provider: check
			// the defaults
			defaultLocation := c.defaultSnapshotLocations[provider]
			if defaultLocation == "" {
				errors = append(errors, fmt.Sprintf("provider %s has more than one possible volume snapshot location, and none were specified explicitly or as a default", provider))
				continue
			}
			location, err := c.snapshotLocationLister.VolumeSnapshotLocations(backup.Namespace).Get(defaultLocation)
			if err != nil {
				errors = append(errors, fmt.Sprintf("error getting volume snapshot location named %s: %v", defaultLocation, err))
				continue
			}

			providerLocations[provider] = location
			continue
		}

		// exactly one location for the provider: use it
		providerLocations[provider] = locations[0]
	}

	if len(errors) > 0 {
		return nil, errors
	}

	return providerLocations, nil
}

func (c *backupController) runBackup(backup *pkgbackup.Request) error {
	log := c.logger.WithField("backup", kubeutil.NamespaceAndName(backup))
	log.Info("Starting backup")
	backup.Status.StartTimestamp.Time = c.clock.Now()

	logFile, err := ioutil.TempFile("", "")
	if err != nil {
		return errors.Wrap(err, "error creating temp file for backup log")
	}
	gzippedLogFile := gzip.NewWriter(logFile)
	// Assuming we successfully uploaded the log file, this will have already been closed below. It is safe to call
	// close multiple times. If we get an error closing this, there's not really anything we can do about it.
	defer gzippedLogFile.Close()
	defer closeAndRemoveFile(logFile, c.logger)

	// Log the backup to both a backup log file and to stdout. This will help see what happened if the upload of the
	// backup log failed for whatever reason.
	logger := logging.DefaultLogger(c.backupLogLevel)
	logger.Out = io.MultiWriter(os.Stdout, gzippedLogFile)
	log = logger.WithField("backup", kubeutil.NamespaceAndName(backup))

	log.Info("Starting backup")

	backupFile, err := ioutil.TempFile("", "")
	if err != nil {
		return errors.Wrap(err, "error creating temp file for backup")
	}
	defer closeAndRemoveFile(backupFile, log)

	pluginManager := c.newPluginManager(log)
	defer pluginManager.CleanupClients()

	actions, err := pluginManager.GetBackupItemActions()
	if err != nil {
		return err
	}

	backupStore, err := c.newBackupStore(backup.StorageLocation, pluginManager, log)
	if err != nil {
		return err
	}

	var errs []error

	// Do the actual backup
	if err := c.backupper.Backup(log, backup, backupFile, actions, pluginManager); err != nil {
		errs = append(errs, err)
		backup.Status.Phase = velerov1api.BackupPhaseFailed
	} else {
		backup.Status.Phase = velerov1api.BackupPhaseCompleted
	}

	if err := gzippedLogFile.Close(); err != nil {
		c.logger.WithError(err).Error("error closing gzippedLogFile")
	}

	// Mark completion timestamp before serializing and uploading.
	// Otherwise, the JSON file in object storage has a CompletionTimestamp of 'null'.
	backup.Status.CompletionTimestamp.Time = c.clock.Now()

	backup.Status.VolumeSnapshotsAttempted = len(backup.VolumeSnapshots)
	for _, snap := range backup.VolumeSnapshots {
		if snap.Status.Phase == volume.SnapshotPhaseCompleted {
			backup.Status.VolumeSnapshotsCompleted++
		}
	}

	errs = append(errs, persistBackup(backup, backupFile, logFile, backupStore, c.logger)...)
	errs = append(errs, recordBackupMetrics(backup.Backup, backupFile, c.metrics))

	log.Info("Backup completed")

	return kerrors.NewAggregate(errs)
}

func recordBackupMetrics(backup *velerov1api.Backup, backupFile *os.File, serverMetrics *metrics.ServerMetrics) error {
	backupScheduleName := backup.GetLabels()[velerov1api.ScheduleNameLabel]

	var backupSizeBytes int64
	var err error
	if backupFileStat, err := backupFile.Stat(); err != nil {
		err = errors.Wrap(err, "error getting file info")
	} else {
		backupSizeBytes = backupFileStat.Size()
	}
	serverMetrics.SetBackupTarballSizeBytesGauge(backupScheduleName, backupSizeBytes)

	backupDuration := backup.Status.CompletionTimestamp.Time.Sub(backup.Status.StartTimestamp.Time)
	backupDurationSeconds := float64(backupDuration / time.Second)
	serverMetrics.RegisterBackupDuration(backupScheduleName, backupDurationSeconds)
	serverMetrics.RegisterVolumeSnapshotAttempts(backupScheduleName, backup.Status.VolumeSnapshotsAttempted)
	serverMetrics.RegisterVolumeSnapshotSuccesses(backupScheduleName, backup.Status.VolumeSnapshotsCompleted)
	serverMetrics.RegisterVolumeSnapshotFailures(backupScheduleName, backup.Status.VolumeSnapshotsAttempted-backup.Status.VolumeSnapshotsCompleted)

	return err
}

func persistBackup(backup *pkgbackup.Request, backupContents, backupLog *os.File, backupStore persistence.BackupStore, log logrus.FieldLogger) []error {
	errs := []error{}
	backupJSON := new(bytes.Buffer)

	if err := encode.EncodeTo(backup.Backup, "json", backupJSON); err != nil {
		errs = append(errs, errors.Wrap(err, "error encoding backup"))
	}

	volumeSnapshots := new(bytes.Buffer)
	gzw := gzip.NewWriter(volumeSnapshots)
	defer gzw.Close()

	if err := json.NewEncoder(gzw).Encode(backup.VolumeSnapshots); err != nil {
		errs = append(errs, errors.Wrap(err, "error encoding list of volume snapshots"))
	}
	if err := gzw.Close(); err != nil {
		errs = append(errs, errors.Wrap(err, "error closing gzip writer"))
	}

	if len(errs) > 0 {
		// Don't upload the JSON files or backup tarball if encoding to json fails.
		backupJSON = nil
		backupContents = nil
		volumeSnapshots = nil
	}

	if err := backupStore.PutBackup(backup.Name, backupJSON, backupContents, backupLog, volumeSnapshots); err != nil {
		errs = append(errs, err)
	}

	return errs
}

func closeAndRemoveFile(file *os.File, log logrus.FieldLogger) {
	if err := file.Close(); err != nil {
		log.WithError(err).WithField("file", file.Name()).Error("error closing file")
	}
	if err := os.Remove(file.Name()); err != nil {
		log.WithError(err).WithField("file", file.Name()).Error("error removing file")
	}
}
