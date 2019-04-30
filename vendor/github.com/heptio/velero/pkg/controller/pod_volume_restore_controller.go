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

package controller

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1api "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	velerov1api "github.com/heptio/velero/pkg/apis/velero/v1"
	velerov1client "github.com/heptio/velero/pkg/generated/clientset/versioned/typed/velero/v1"
	informers "github.com/heptio/velero/pkg/generated/informers/externalversions/velero/v1"
	listers "github.com/heptio/velero/pkg/generated/listers/velero/v1"
	"github.com/heptio/velero/pkg/restic"
	"github.com/heptio/velero/pkg/util/boolptr"
	veleroexec "github.com/heptio/velero/pkg/util/exec"
	"github.com/heptio/velero/pkg/util/filesystem"
	"github.com/heptio/velero/pkg/util/kube"
)

type podVolumeRestoreController struct {
	*genericController

	podVolumeRestoreClient velerov1client.PodVolumeRestoresGetter
	podVolumeRestoreLister listers.PodVolumeRestoreLister
	podLister              corev1listers.PodLister
	secretLister           corev1listers.SecretLister
	pvcLister              corev1listers.PersistentVolumeClaimLister
	backupLocationLister   listers.BackupStorageLocationLister
	nodeName               string

	processRestoreFunc func(*velerov1api.PodVolumeRestore) error
	fileSystem         filesystem.Interface
}

// NewPodVolumeRestoreController creates a new pod volume restore controller.
func NewPodVolumeRestoreController(
	logger logrus.FieldLogger,
	podVolumeRestoreInformer informers.PodVolumeRestoreInformer,
	podVolumeRestoreClient velerov1client.PodVolumeRestoresGetter,
	podInformer cache.SharedIndexInformer,
	secretInformer cache.SharedIndexInformer,
	pvcInformer corev1informers.PersistentVolumeClaimInformer,
	backupLocationInformer informers.BackupStorageLocationInformer,
	nodeName string,
) Interface {
	c := &podVolumeRestoreController{
		genericController:      newGenericController("pod-volume-restore", logger),
		podVolumeRestoreClient: podVolumeRestoreClient,
		podVolumeRestoreLister: podVolumeRestoreInformer.Lister(),
		podLister:              corev1listers.NewPodLister(podInformer.GetIndexer()),
		secretLister:           corev1listers.NewSecretLister(secretInformer.GetIndexer()),
		pvcLister:              pvcInformer.Lister(),
		backupLocationLister:   backupLocationInformer.Lister(),
		nodeName:               nodeName,

		fileSystem: filesystem.NewFileSystem(),
	}

	c.syncHandler = c.processQueueItem
	c.cacheSyncWaiters = append(
		c.cacheSyncWaiters,
		podVolumeRestoreInformer.Informer().HasSynced,
		podInformer.HasSynced,
		secretInformer.HasSynced,
		pvcInformer.Informer().HasSynced,
		backupLocationInformer.Informer().HasSynced,
	)
	c.processRestoreFunc = c.processRestore

	podVolumeRestoreInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: c.pvrHandler,
			UpdateFunc: func(_, obj interface{}) {
				c.pvrHandler(obj)
			},
		},
	)

	podInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: c.podHandler,
			UpdateFunc: func(_, obj interface{}) {
				c.podHandler(obj)
			},
		},
	)

	return c
}

func (c *podVolumeRestoreController) pvrHandler(obj interface{}) {
	pvr := obj.(*velerov1api.PodVolumeRestore)
	log := loggerForPodVolumeRestore(c.logger, pvr)

	if !isPVRNew(pvr) {
		log.Debugf("Restore is not new, not enqueuing")
		return
	}

	pod, err := c.podLister.Pods(pvr.Spec.Pod.Namespace).Get(pvr.Spec.Pod.Name)
	if apierrors.IsNotFound(err) {
		log.WithError(err).Debugf("Restore's pod %s/%s not found, not enqueueing.", pvr.Spec.Pod.Namespace, pvr.Spec.Pod.Name)
		return
	}
	if err != nil {
		log.WithError(err).Errorf("Unable to get restore's pod %s/%s, not enqueueing.", pvr.Spec.Pod.Namespace, pvr.Spec.Pod.Name)
		return
	}

	if !isPodOnNode(pod, c.nodeName) {
		log.Debugf("Restore's pod is not on this node, not enqueuing")
		return
	}

	if !isResticInitContainerRunning(pod) {
		log.Debug("Restore's pod is not running restic-wait init container, not enqueuing")
		return
	}

	log.Debug("Enqueueing")
	c.enqueue(obj)
}

func (c *podVolumeRestoreController) podHandler(obj interface{}) {
	pod := obj.(*corev1api.Pod)
	log := c.logger.WithField("key", kube.NamespaceAndName(pod))

	// the pod should always be for this node since the podInformer is filtered
	// based on node, so this is just a failsafe.
	if !isPodOnNode(pod, c.nodeName) {
		return
	}

	if !isResticInitContainerRunning(pod) {
		log.Debug("Pod is not running restic-wait init container, not enqueuing restores for pod")
		return
	}

	selector := labels.Set(map[string]string{
		velerov1api.PodUIDLabel: string(pod.UID),
	}).AsSelector()

	pvrs, err := c.podVolumeRestoreLister.List(selector)
	if err != nil {
		log.WithError(err).Error("Unable to list pod volume restores")
		return
	}

	if len(pvrs) == 0 {
		return
	}

	for _, pvr := range pvrs {
		log := loggerForPodVolumeRestore(log, pvr)
		if !isPVRNew(pvr) {
			log.Debug("Restore is not new, not enqueuing")
			continue
		}
		log.Debug("Enqueuing")
		c.enqueue(pvr)
	}
}

func isPVRNew(pvr *velerov1api.PodVolumeRestore) bool {
	return pvr.Status.Phase == "" || pvr.Status.Phase == velerov1api.PodVolumeRestorePhaseNew
}

func isPodOnNode(pod *corev1api.Pod, node string) bool {
	return pod.Spec.NodeName == node
}

func isResticInitContainerRunning(pod *corev1api.Pod) bool {
	// no init containers, or the first one is not the velero restic one: return false
	if len(pod.Spec.InitContainers) == 0 || pod.Spec.InitContainers[0].Name != restic.InitContainer {
		return false
	}

	// status hasn't been created yet, or the first one is not yet running: return false
	if len(pod.Status.InitContainerStatuses) == 0 || pod.Status.InitContainerStatuses[0].State.Running == nil {
		return false
	}

	// else, it's running
	return true
}

func (c *podVolumeRestoreController) processQueueItem(key string) error {
	log := c.logger.WithField("key", key)
	log.Debug("Running processQueueItem")

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		log.WithError(errors.WithStack(err)).Error("error splitting queue key")
		return nil
	}

	req, err := c.podVolumeRestoreLister.PodVolumeRestores(ns).Get(name)
	if apierrors.IsNotFound(err) {
		log.Debug("Unable to find PodVolumeRestore")
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "error getting PodVolumeRestore")
	}

	// Don't mutate the shared cache
	reqCopy := req.DeepCopy()
	return c.processRestoreFunc(reqCopy)
}

func loggerForPodVolumeRestore(baseLogger logrus.FieldLogger, req *velerov1api.PodVolumeRestore) logrus.FieldLogger {
	log := baseLogger.WithFields(logrus.Fields{
		"namespace": req.Namespace,
		"name":      req.Name,
	})

	if len(req.OwnerReferences) == 1 {
		log = log.WithField("restore", fmt.Sprintf("%s/%s", req.Namespace, req.OwnerReferences[0].Name))
	}

	return log
}

func (c *podVolumeRestoreController) processRestore(req *velerov1api.PodVolumeRestore) error {
	log := loggerForPodVolumeRestore(c.logger, req)

	log.Info("Restore starting")

	var err error

	// update status to InProgress
	req, err = c.patchPodVolumeRestore(req, updatePodVolumeRestorePhaseFunc(velerov1api.PodVolumeRestorePhaseInProgress))
	if err != nil {
		log.WithError(err).Error("Error setting phase to InProgress")
		return errors.WithStack(err)
	}

	pod, err := c.podLister.Pods(req.Spec.Pod.Namespace).Get(req.Spec.Pod.Name)
	if err != nil {
		log.WithError(err).Errorf("Error getting pod %s/%s", req.Spec.Pod.Namespace, req.Spec.Pod.Name)
		return c.failRestore(req, errors.Wrap(err, "error getting pod").Error(), log)
	}

	volumeDir, err := kube.GetVolumeDirectory(pod, req.Spec.Volume, c.pvcLister)
	if err != nil {
		log.WithError(err).Error("Error getting volume directory name")
		return c.failRestore(req, errors.Wrap(err, "error getting volume directory name").Error(), log)
	}

	credsFile, err := restic.TempCredentialsFile(c.secretLister, req.Namespace, req.Spec.Pod.Namespace, c.fileSystem)
	if err != nil {
		log.WithError(err).Error("Error creating temp restic credentials file")
		return c.failRestore(req, errors.Wrap(err, "error creating temp restic credentials file").Error(), log)
	}
	// ignore error since there's nothing we can do and it's a temp file.
	defer os.Remove(credsFile)

	// execute the restore process
	if err := c.restorePodVolume(req, credsFile, volumeDir, log); err != nil {
		log.WithError(err).Error("Error restoring volume")
		return c.failRestore(req, errors.Wrap(err, "error restoring volume").Error(), log)
	}

	// update status to Completed
	if _, err = c.patchPodVolumeRestore(req, updatePodVolumeRestorePhaseFunc(velerov1api.PodVolumeRestorePhaseCompleted)); err != nil {
		log.WithError(err).Error("Error setting phase to Completed")
		return err
	}

	log.Info("Restore completed")

	return nil
}

func (c *podVolumeRestoreController) restorePodVolume(req *velerov1api.PodVolumeRestore, credsFile, volumeDir string, log logrus.FieldLogger) error {
	// Get the full path of the new volume's directory as mounted in the daemonset pod, which
	// will look like: /host_pods/<new-pod-uid>/volumes/<volume-plugin-name>/<volume-dir>
	volumePath, err := singlePathMatch(fmt.Sprintf("/host_pods/%s/volumes/*/%s", string(req.Spec.Pod.UID), volumeDir))
	if err != nil {
		return errors.Wrap(err, "error identifying path of volume")
	}

	resticCmd := restic.RestoreCommand(
		req.Spec.RepoIdentifier,
		credsFile,
		req.Spec.SnapshotID,
		volumePath,
	)

	// if this is azure, set resticCmd.Env appropriately
	if strings.HasPrefix(req.Spec.RepoIdentifier, "azure") {
		env, err := restic.AzureCmdEnv(c.backupLocationLister, req.Namespace, req.Spec.BackupStorageLocation)
		if err != nil {
			return c.failRestore(req, errors.Wrap(err, "error setting restic cmd env").Error(), log)
		}
		resticCmd.Env = env
	}

	var stdout, stderr string

	if stdout, stderr, err = veleroexec.RunCommand(resticCmd.Cmd()); err != nil {
		return errors.Wrapf(err, "error running restic restore, cmd=%s, stdout=%s, stderr=%s", resticCmd.String(), stdout, stderr)
	}
	log.Debugf("Ran command=%s, stdout=%s, stderr=%s", resticCmd.String(), stdout, stderr)

	// Remove the .velero directory from the restored volume (it may contain done files from previous restores
	// of this volume, which we don't want to carry over). If this fails for any reason, log and continue, since
	// this is non-essential cleanup (the done files are named based on restore UID and the init container looks
	// for the one specific to the restore being executed).
	if err := os.RemoveAll(filepath.Join(volumePath, ".velero")); err != nil {
		log.WithError(err).Warnf("error removing .velero directory from directory %s", volumePath)
	}

	var restoreUID types.UID
	for _, owner := range req.OwnerReferences {
		if boolptr.IsSetToTrue(owner.Controller) {
			restoreUID = owner.UID
			break
		}
	}

	// Create the .velero directory within the volume dir so we can write a done file
	// for this restore.
	if err := os.MkdirAll(filepath.Join(volumePath, ".velero"), 0755); err != nil {
		return errors.Wrap(err, "error creating .velero directory for done file")
	}

	// Write a done file with name=<restore-uid> into the just-created .velero dir
	// within the volume. The velero restic init container on the pod is waiting
	// for this file to exist in each restored volume before completing.
	if err := ioutil.WriteFile(filepath.Join(volumePath, ".velero", string(restoreUID)), nil, 0644); err != nil {
		return errors.Wrap(err, "error writing done file")
	}

	return nil
}

func (c *podVolumeRestoreController) patchPodVolumeRestore(req *velerov1api.PodVolumeRestore, mutate func(*velerov1api.PodVolumeRestore)) (*velerov1api.PodVolumeRestore, error) {
	// Record original json
	oldData, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original PodVolumeRestore")
	}

	// Mutate
	mutate(req)

	// Record new json
	newData, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated PodVolumeRestore")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for PodVolumeRestore")
	}

	req, err = c.podVolumeRestoreClient.PodVolumeRestores(req.Namespace).Patch(req.Name, types.MergePatchType, patchBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error patching PodVolumeRestore")
	}

	return req, nil
}

func (c *podVolumeRestoreController) failRestore(req *velerov1api.PodVolumeRestore, msg string, log logrus.FieldLogger) error {
	if _, err := c.patchPodVolumeRestore(req, func(pvr *velerov1api.PodVolumeRestore) {
		pvr.Status.Phase = velerov1api.PodVolumeRestorePhaseFailed
		pvr.Status.Message = msg
	}); err != nil {
		log.WithError(err).Error("Error setting phase to Failed")
		return err
	}
	return nil
}

func updatePodVolumeRestorePhaseFunc(phase velerov1api.PodVolumeRestorePhase) func(r *velerov1api.PodVolumeRestore) {
	return func(r *velerov1api.PodVolumeRestore) {
		r.Status.Phase = phase
	}
}
