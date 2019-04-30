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
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	velerov1api "github.com/heptio/velero/pkg/apis/velero/v1"
	clientset "github.com/heptio/velero/pkg/generated/clientset/versioned"
	velerov1client "github.com/heptio/velero/pkg/generated/clientset/versioned/typed/velero/v1"
	velerov1informers "github.com/heptio/velero/pkg/generated/informers/externalversions/velero/v1"
	velerov1listers "github.com/heptio/velero/pkg/generated/listers/velero/v1"
	veleroexec "github.com/heptio/velero/pkg/util/exec"
	"github.com/heptio/velero/pkg/util/filesystem"
)

// RepositoryManager executes commands against restic repositories.
type RepositoryManager interface {
	// InitRepo initializes a repo with the specified name and identifier.
	InitRepo(repo *velerov1api.ResticRepository) error

	// ConnectToRepo runs the 'restic stats' command against the
	// specified repo, and returns an error if it fails. This is
	// intended to be used to ensure that the repo exists/can be
	// authenticated to.
	ConnectToRepo(repo *velerov1api.ResticRepository) error

	// CheckRepo checks the specified repo for errors.
	CheckRepo(repo *velerov1api.ResticRepository) error

	// PruneRepo deletes unused data from a repo.
	PruneRepo(repo *velerov1api.ResticRepository) error

	// Forget removes a snapshot from the list of
	// available snapshots in a repo.
	Forget(context.Context, SnapshotIdentifier) error

	BackupperFactory

	RestorerFactory
}

// BackupperFactory can construct restic backuppers.
type BackupperFactory interface {
	// NewBackupper returns a restic backupper for use during a single
	// Velero backup.
	NewBackupper(context.Context, *velerov1api.Backup) (Backupper, error)
}

// RestorerFactory can construct restic restorers.
type RestorerFactory interface {
	// NewRestorer returns a restic restorer for use during a single
	// Velero restore.
	NewRestorer(context.Context, *velerov1api.Restore) (Restorer, error)
}

type repositoryManager struct {
	namespace                    string
	veleroClient                 clientset.Interface
	secretsLister                corev1listers.SecretLister
	repoLister                   velerov1listers.ResticRepositoryLister
	repoInformerSynced           cache.InformerSynced
	backupLocationLister         velerov1listers.BackupStorageLocationLister
	backupLocationInformerSynced cache.InformerSynced
	log                          logrus.FieldLogger
	repoLocker                   *repoLocker
	repoEnsurer                  *repositoryEnsurer
	fileSystem                   filesystem.Interface
	ctx                          context.Context
}

// NewRepositoryManager constructs a RepositoryManager.
func NewRepositoryManager(
	ctx context.Context,
	namespace string,
	veleroClient clientset.Interface,
	secretsInformer cache.SharedIndexInformer,
	repoInformer velerov1informers.ResticRepositoryInformer,
	repoClient velerov1client.ResticRepositoriesGetter,
	backupLocationInformer velerov1informers.BackupStorageLocationInformer,
	log logrus.FieldLogger,
) (RepositoryManager, error) {
	rm := &repositoryManager{
		namespace:                    namespace,
		veleroClient:                 veleroClient,
		secretsLister:                corev1listers.NewSecretLister(secretsInformer.GetIndexer()),
		repoLister:                   repoInformer.Lister(),
		repoInformerSynced:           repoInformer.Informer().HasSynced,
		backupLocationLister:         backupLocationInformer.Lister(),
		backupLocationInformerSynced: backupLocationInformer.Informer().HasSynced,
		log:                          log,
		ctx:                          ctx,

		repoLocker:  newRepoLocker(),
		repoEnsurer: newRepositoryEnsurer(repoInformer, repoClient, log),
		fileSystem:  filesystem.NewFileSystem(),
	}

	if !cache.WaitForCacheSync(ctx.Done(), secretsInformer.HasSynced) {
		return nil, errors.New("timed out waiting for cache to sync")
	}

	return rm, nil
}

func (rm *repositoryManager) NewBackupper(ctx context.Context, backup *velerov1api.Backup) (Backupper, error) {
	informer := velerov1informers.NewFilteredPodVolumeBackupInformer(
		rm.veleroClient,
		backup.Namespace,
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		func(opts *metav1.ListOptions) {
			opts.LabelSelector = fmt.Sprintf("%s=%s", velerov1api.BackupUIDLabel, backup.UID)
		},
	)

	b := newBackupper(ctx, rm, rm.repoEnsurer, informer, rm.log)

	go informer.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced, rm.repoInformerSynced) {
		return nil, errors.New("timed out waiting for caches to sync")
	}

	return b, nil
}

func (rm *repositoryManager) NewRestorer(ctx context.Context, restore *velerov1api.Restore) (Restorer, error) {
	informer := velerov1informers.NewFilteredPodVolumeRestoreInformer(
		rm.veleroClient,
		restore.Namespace,
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		func(opts *metav1.ListOptions) {
			opts.LabelSelector = fmt.Sprintf("%s=%s", velerov1api.RestoreUIDLabel, restore.UID)
		},
	)

	r := newRestorer(ctx, rm, rm.repoEnsurer, informer, rm.log)

	go informer.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced, rm.repoInformerSynced) {
		return nil, errors.New("timed out waiting for cache to sync")
	}

	return r, nil
}

func (rm *repositoryManager) InitRepo(repo *velerov1api.ResticRepository) error {
	// restic init requires an exclusive lock
	rm.repoLocker.LockExclusive(repo.Name)
	defer rm.repoLocker.UnlockExclusive(repo.Name)

	return rm.exec(InitCommand(repo.Spec.ResticIdentifier), repo.Spec.BackupStorageLocation)
}

func (rm *repositoryManager) ConnectToRepo(repo *velerov1api.ResticRepository) error {
	// restic stats requires a non-exclusive lock
	rm.repoLocker.Lock(repo.Name)
	defer rm.repoLocker.Unlock(repo.Name)

	return rm.exec(StatsCommand(repo.Spec.ResticIdentifier), repo.Spec.BackupStorageLocation)
}

func (rm *repositoryManager) CheckRepo(repo *velerov1api.ResticRepository) error {
	// restic check requires an exclusive lock
	rm.repoLocker.LockExclusive(repo.Name)
	defer rm.repoLocker.UnlockExclusive(repo.Name)

	return rm.exec(CheckCommand(repo.Spec.ResticIdentifier), repo.Spec.BackupStorageLocation)
}

func (rm *repositoryManager) PruneRepo(repo *velerov1api.ResticRepository) error {
	// restic prune requires an exclusive lock
	rm.repoLocker.LockExclusive(repo.Name)
	defer rm.repoLocker.UnlockExclusive(repo.Name)

	return rm.exec(PruneCommand(repo.Spec.ResticIdentifier), repo.Spec.BackupStorageLocation)
}

func (rm *repositoryManager) Forget(ctx context.Context, snapshot SnapshotIdentifier) error {
	// We can't wait for this in the constructor, because this informer is coming
	// from the shared informer factory, which isn't started until *after* the repo
	// manager is instantiated & passed to the controller constructors. We'd get a
	// deadlock if we tried to wait for this in the constructor.
	if !cache.WaitForCacheSync(ctx.Done(), rm.repoInformerSynced) {
		return errors.New("timed out waiting for cache to sync")
	}

	repo, err := rm.repoEnsurer.EnsureRepo(ctx, rm.namespace, snapshot.VolumeNamespace, snapshot.BackupStorageLocation)
	if err != nil {
		return err
	}

	// restic forget requires an exclusive lock
	rm.repoLocker.LockExclusive(repo.Name)
	defer rm.repoLocker.UnlockExclusive(repo.Name)

	return rm.exec(ForgetCommand(repo.Spec.ResticIdentifier, snapshot.SnapshotID), repo.Spec.BackupStorageLocation)
}

func (rm *repositoryManager) exec(cmd *Command, backupLocation string) error {
	file, err := TempCredentialsFile(rm.secretsLister, rm.namespace, cmd.RepoName(), rm.fileSystem)
	if err != nil {
		return err
	}
	// ignore error since there's nothing we can do and it's a temp file.
	defer os.Remove(file)

	cmd.PasswordFile = file

	if strings.HasPrefix(cmd.RepoIdentifier, "azure") {
		if !cache.WaitForCacheSync(rm.ctx.Done(), rm.backupLocationInformerSynced) {
			return errors.New("timed out waiting for cache to sync")
		}

		env, err := AzureCmdEnv(rm.backupLocationLister, rm.namespace, backupLocation)
		if err != nil {
			return err
		}
		cmd.Env = env
	}

	stdout, stderr, err := veleroexec.RunCommand(cmd.Cmd())
	rm.log.WithFields(logrus.Fields{
		"repository": cmd.RepoName(),
		"command":    cmd.String(),
		"stdout":     stdout,
		"stderr":     stderr,
	}).Debugf("Ran restic command")
	if err != nil {
		return errors.Wrapf(err, "error running command=%s, stdout=%s, stderr=%s", cmd.String(), stdout, stderr)
	}

	return nil
}
