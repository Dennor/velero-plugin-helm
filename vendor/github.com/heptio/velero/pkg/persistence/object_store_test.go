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

package persistence

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	arkv1api "github.com/heptio/velero/pkg/apis/ark/v1"
	velerov1api "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/cloudprovider"
	cloudprovidermocks "github.com/heptio/velero/pkg/cloudprovider/mocks"
	"github.com/heptio/velero/pkg/util/encode"
	velerotest "github.com/heptio/velero/pkg/util/test"
	"github.com/heptio/velero/pkg/volume"
)

type objectBackupStoreTestHarness struct {
	// embedded to reduce verbosity when calling methods
	*objectBackupStore

	objectStore    *cloudprovider.InMemoryObjectStore
	bucket, prefix string
}

func newObjectBackupStoreTestHarness(bucket, prefix string) *objectBackupStoreTestHarness {
	objectStore := cloudprovider.NewInMemoryObjectStore(bucket)

	return &objectBackupStoreTestHarness{
		objectBackupStore: &objectBackupStore{
			objectStore: objectStore,
			bucket:      bucket,
			layout:      NewObjectStoreLayout(prefix),
			logger:      velerotest.NewLogger(),
		},
		objectStore: objectStore,
		bucket:      bucket,
		prefix:      prefix,
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		storageData cloudprovider.BucketData
		expectErr   bool
	}{
		{
			name:      "empty backup store with no prefix is valid",
			expectErr: false,
		},
		{
			name:      "empty backup store with a prefix is valid",
			prefix:    "bar",
			expectErr: false,
		},
		{
			name: "backup store with no prefix and only unsupported directories is invalid",
			storageData: map[string][]byte{
				"backup-1/velero-backup.json": {},
				"backup-2/velero-backup.json": {},
			},
			expectErr: true,
		},
		{
			name:   "backup store with a prefix and only unsupported directories is invalid",
			prefix: "backups",
			storageData: map[string][]byte{
				"backups/backup-1/velero-backup.json": {},
				"backups/backup-2/velero-backup.json": {},
			},
			expectErr: true,
		},
		{
			name: "backup store with no prefix and both supported and unsupported directories is invalid",
			storageData: map[string][]byte{
				"backups/backup-1/velero-backup.json": {},
				"backups/backup-2/velero-backup.json": {},
				"restores/restore-1/foo":              {},
				"unsupported-dir/foo":                 {},
			},
			expectErr: true,
		},
		{
			name:   "backup store with a prefix and both supported and unsupported directories is invalid",
			prefix: "cluster-1",
			storageData: map[string][]byte{
				"cluster-1/backups/backup-1/velero-backup.json": {},
				"cluster-1/backups/backup-2/velero-backup.json": {},
				"cluster-1/restores/restore-1/foo":              {},
				"cluster-1/unsupported-dir/foo":                 {},
			},
			expectErr: true,
		},
		{
			name: "backup store with no prefix and only supported directories is valid",
			storageData: map[string][]byte{
				"backups/backup-1/velero-backup.json": {},
				"backups/backup-2/velero-backup.json": {},
				"restores/restore-1/foo":              {},
			},
			expectErr: false,
		},
		{
			name:   "backup store with a prefix and only supported directories is valid",
			prefix: "cluster-1",
			storageData: map[string][]byte{
				"cluster-1/backups/backup-1/velero-backup.json": {},
				"cluster-1/backups/backup-2/velero-backup.json": {},
				"cluster-1/restores/restore-1/foo":              {},
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := newObjectBackupStoreTestHarness("foo", tc.prefix)

			for key, obj := range tc.storageData {
				require.NoError(t, harness.objectStore.PutObject(harness.bucket, key, bytes.NewReader(obj)))
			}

			err := harness.IsValid()
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestListBackups(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		storageData cloudprovider.BucketData
		expectedRes []string
		expectedErr string
	}{
		{
			name: "normal case",
			storageData: map[string][]byte{
				"backups/backup-1/velero-backup.json": encodeToBytes(&velerov1api.Backup{ObjectMeta: metav1.ObjectMeta{Name: "backup-1"}}),
				"backups/backup-2/velero-backup.json": encodeToBytes(&velerov1api.Backup{ObjectMeta: metav1.ObjectMeta{Name: "backup-2"}}),
			},
			expectedRes: []string{"backup-1", "backup-2"},
		},
		{
			name:   "normal case with backup store prefix",
			prefix: "velero-backups/",
			storageData: map[string][]byte{
				"velero-backups/backups/backup-1/velero-backup.json": encodeToBytes(&velerov1api.Backup{ObjectMeta: metav1.ObjectMeta{Name: "backup-1"}}),
				"velero-backups/backups/backup-2/velero-backup.json": encodeToBytes(&velerov1api.Backup{ObjectMeta: metav1.ObjectMeta{Name: "backup-2"}}),
			},
			expectedRes: []string{"backup-1", "backup-2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := newObjectBackupStoreTestHarness("foo", tc.prefix)

			for key, obj := range tc.storageData {
				require.NoError(t, harness.objectStore.PutObject(harness.bucket, key, bytes.NewReader(obj)))
			}

			res, err := harness.ListBackups()

			velerotest.AssertErrorMatches(t, tc.expectedErr, err)

			sort.Strings(tc.expectedRes)
			sort.Strings(res)

			assert.Equal(t, tc.expectedRes, res)
		})
	}
}

func TestPutBackup(t *testing.T) {
	tests := []struct {
		name         string
		prefix       string
		metadata     io.Reader
		contents     io.Reader
		log          io.Reader
		snapshots    io.Reader
		expectedErr  string
		expectedKeys []string
	}{
		{
			name:        "normal case",
			metadata:    newStringReadSeeker("metadata"),
			contents:    newStringReadSeeker("contents"),
			log:         newStringReadSeeker("log"),
			snapshots:   newStringReadSeeker("snapshots"),
			expectedErr: "",
			expectedKeys: []string{
				"backups/backup-1/velero-backup.json",
				"backups/backup-1/backup-1.tar.gz",
				"backups/backup-1/backup-1-logs.gz",
				"backups/backup-1/backup-1-volumesnapshots.json.gz",
				"metadata/revision",
			},
		},
		{
			name:        "normal case with backup store prefix",
			prefix:      "prefix-1/",
			metadata:    newStringReadSeeker("metadata"),
			contents:    newStringReadSeeker("contents"),
			log:         newStringReadSeeker("log"),
			snapshots:   newStringReadSeeker("snapshots"),
			expectedErr: "",
			expectedKeys: []string{
				"prefix-1/backups/backup-1/velero-backup.json",
				"prefix-1/backups/backup-1/backup-1.tar.gz",
				"prefix-1/backups/backup-1/backup-1-logs.gz",
				"prefix-1/backups/backup-1/backup-1-volumesnapshots.json.gz",
				"prefix-1/metadata/revision",
			},
		},
		{
			name:         "error on metadata upload does not upload data",
			metadata:     new(errorReader),
			contents:     newStringReadSeeker("contents"),
			log:          newStringReadSeeker("log"),
			snapshots:    newStringReadSeeker("snapshots"),
			expectedErr:  "error readers return errors",
			expectedKeys: []string{"backups/backup-1/backup-1-logs.gz"},
		},
		{
			name:         "error on data upload deletes metadata",
			metadata:     newStringReadSeeker("metadata"),
			contents:     new(errorReader),
			log:          newStringReadSeeker("log"),
			snapshots:    newStringReadSeeker("snapshots"),
			expectedErr:  "error readers return errors",
			expectedKeys: []string{"backups/backup-1/backup-1-logs.gz"},
		},
		{
			name:        "error on log upload is ok",
			metadata:    newStringReadSeeker("foo"),
			contents:    newStringReadSeeker("bar"),
			log:         new(errorReader),
			snapshots:   newStringReadSeeker("snapshots"),
			expectedErr: "",
			expectedKeys: []string{
				"backups/backup-1/velero-backup.json",
				"backups/backup-1/backup-1.tar.gz",
				"backups/backup-1/backup-1-volumesnapshots.json.gz",
				"metadata/revision",
			},
		},
		{
			name:         "don't upload data when metadata is nil",
			metadata:     nil,
			contents:     newStringReadSeeker("contents"),
			log:          newStringReadSeeker("log"),
			snapshots:    newStringReadSeeker("snapshots"),
			expectedErr:  "",
			expectedKeys: []string{"backups/backup-1/backup-1-logs.gz"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := newObjectBackupStoreTestHarness("foo", tc.prefix)

			err := harness.PutBackup("backup-1", tc.metadata, tc.contents, tc.log, tc.snapshots)

			velerotest.AssertErrorMatches(t, tc.expectedErr, err)
			assert.Len(t, harness.objectStore.Data[harness.bucket], len(tc.expectedKeys))
			for _, key := range tc.expectedKeys {
				assert.Contains(t, harness.objectStore.Data[harness.bucket], key)
			}
		})
	}
}

func TestGetBackupMetadata(t *testing.T) {
	tests := []struct {
		name       string
		backupName string
		key        string
		obj        metav1.Object
		wantErr    error
	}{
		{
			name:       "legacy metadata file returns correctly",
			backupName: "foo",
			key:        "backups/foo/ark-backup.json",
			obj: &arkv1api.Backup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Backup",
					APIVersion: arkv1api.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: arkv1api.DefaultNamespace,
					Name:      "foo",
				},
			},
		},
		{
			name:       "current metadata file returns correctly",
			backupName: "foo",
			key:        "backups/foo/velero-backup.json",
			obj: &velerov1api.Backup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Backup",
					APIVersion: velerov1api.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: velerov1api.DefaultNamespace,
					Name:      "foo",
				},
			},
		},
		{
			name:       "no metadata file returns an error",
			backupName: "foo",
			wantErr:    errors.New("key not found"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := newObjectBackupStoreTestHarness("test-bucket", "")

			if tc.obj != nil {
				jsonBytes, err := json.Marshal(tc.obj)
				require.NoError(t, err)

				require.NoError(t, harness.objectStore.PutObject(harness.bucket, tc.key, bytes.NewReader(jsonBytes)))
			}

			res, err := harness.GetBackupMetadata(tc.backupName)
			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)
			} else {
				require.NoError(t, err)

				assert.Equal(t, tc.obj.GetNamespace(), res.Namespace)
				assert.Equal(t, tc.obj.GetName(), res.Name)
			}
		})
	}
}

func TestGetAndConvertLegacyBackupMetadata(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		obj     metav1.Object
		want    *velerov1api.Backup
		wantErr error
	}{
		{
			name: "velerov1api group, labels and annotations all get converted",
			key:  "backups/foo/ark-backup.json",
			obj: &arkv1api.Backup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Backup",
					APIVersion: arkv1api.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: arkv1api.DefaultNamespace,
					Name:      "foo",
					Labels: map[string]string{
						"ark.heptio.com/foo":        "bar",
						"ark.heptio.com/tango":      "foxtrot",
						"prefix.ark.heptio.com/zaz": "zoo",
						"non-matching":              "no-change",
					},
					Annotations: map[string]string{
						"ark.heptio.com/foo":        "bar",
						"ark.heptio.com/tango":      "foxtrot",
						"prefix.ark.heptio.com/zaz": "zoo",
						"non-matching":              "no-change",
					},
				},
			},
			want: &velerov1api.Backup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Backup",
					APIVersion: velerov1api.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: arkv1api.DefaultNamespace,
					Name:      "foo",
					Labels: map[string]string{
						"velero.io/foo":        "bar",
						"velero.io/tango":      "foxtrot",
						"prefix.velero.io/zaz": "zoo",
						"non-matching":         "no-change",
					},
					Annotations: map[string]string{
						"velero.io/foo":        "bar",
						"velero.io/tango":      "foxtrot",
						"prefix.velero.io/zaz": "zoo",
						"non-matching":         "no-change",
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := newObjectBackupStoreTestHarness("test-bucket", "")

			jsonBytes, err := json.Marshal(tc.obj)
			require.NoError(t, err)

			require.NoError(t, harness.objectStore.PutObject(harness.bucket, tc.key, bytes.NewReader(jsonBytes)))

			res, err := harness.getAndConvertLegacyBackupMetadata(tc.key)
			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)
			} else {
				require.NoError(t, err)

				assert.Equal(t, tc.want, res)
			}
		})
	}
}

func TestGetBackupVolumeSnapshots(t *testing.T) {
	harness := newObjectBackupStoreTestHarness("test-bucket", "")

	// volumesnapshots file not found should not error
	harness.objectStore.PutObject(harness.bucket, "backups/test-backup/velero-backup.json", newStringReadSeeker("foo"))
	res, err := harness.GetBackupVolumeSnapshots("test-backup")
	assert.NoError(t, err)
	assert.Nil(t, res)

	// volumesnapshots file containing invalid data should error
	harness.objectStore.PutObject(harness.bucket, "backups/test-backup/test-backup-volumesnapshots.json.gz", newStringReadSeeker("foo"))
	res, err = harness.GetBackupVolumeSnapshots("test-backup")
	assert.NotNil(t, err)

	// volumesnapshots file containing gzipped json data should return correctly
	snapshots := []*volume.Snapshot{
		{
			Spec: volume.SnapshotSpec{
				BackupName:           "test-backup",
				PersistentVolumeName: "pv-1",
			},
		},
		{
			Spec: volume.SnapshotSpec{
				BackupName:           "test-backup",
				PersistentVolumeName: "pv-2",
			},
		},
	}

	obj := new(bytes.Buffer)
	gzw := gzip.NewWriter(obj)

	require.NoError(t, json.NewEncoder(gzw).Encode(snapshots))
	require.NoError(t, gzw.Close())
	require.NoError(t, harness.objectStore.PutObject(harness.bucket, "backups/test-backup/test-backup-volumesnapshots.json.gz", obj))

	res, err = harness.GetBackupVolumeSnapshots("test-backup")
	assert.NoError(t, err)
	assert.EqualValues(t, snapshots, res)
}

func TestGetBackupContents(t *testing.T) {
	harness := newObjectBackupStoreTestHarness("test-bucket", "")

	harness.objectStore.PutObject(harness.bucket, "backups/test-backup/test-backup.tar.gz", newStringReadSeeker("foo"))

	rc, err := harness.GetBackupContents("test-backup")
	require.NoError(t, err)
	require.NotNil(t, rc)

	data, err := ioutil.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "foo", string(data))
}

func TestDeleteBackup(t *testing.T) {
	tests := []struct {
		name             string
		prefix           string
		listObjectsError error
		deleteErrors     []error
		expectedErr      string
	}{
		{
			name: "normal case",
		},
		{
			name:   "normal case with backup store prefix",
			prefix: "velero-backups/",
		},
		{
			name:         "some delete errors, do as much as we can",
			deleteErrors: []error{errors.New("a"), nil, errors.New("c")},
			expectedErr:  "[a, c]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objectStore := new(cloudprovidermocks.ObjectStore)
			backupStore := &objectBackupStore{
				objectStore: objectStore,
				bucket:      "test-bucket",
				layout:      NewObjectStoreLayout(test.prefix),
				logger:      velerotest.NewLogger(),
			}
			defer objectStore.AssertExpectations(t)

			objects := []string{test.prefix + "backups/bak/velero-backup.json", test.prefix + "backups/bak/bak.tar.gz", test.prefix + "backups/bak/bak.log.gz"}

			objectStore.On("ListObjects", backupStore.bucket, test.prefix+"backups/bak/").Return(objects, test.listObjectsError)
			for i, obj := range objects {
				var err error
				if i < len(test.deleteErrors) {
					err = test.deleteErrors[i]
				}

				objectStore.On("DeleteObject", backupStore.bucket, obj).Return(err)
				objectStore.On("PutObject", "test-bucket", path.Join(test.prefix, "metadata", "revision"), mock.Anything).Return(nil)
			}

			err := backupStore.DeleteBackup("bak")

			velerotest.AssertErrorMatches(t, test.expectedErr, err)
		})
	}
}

func TestGetDownloadURL(t *testing.T) {
	tests := []struct {
		name        string
		targetKind  velerov1api.DownloadTargetKind
		targetName  string
		prefix      string
		expectedKey string
	}{
		{
			name:        "backup contents",
			targetKind:  velerov1api.DownloadTargetKindBackupContents,
			targetName:  "my-backup",
			expectedKey: "backups/my-backup/my-backup.tar.gz",
		},
		{
			name:        "backup log",
			targetKind:  velerov1api.DownloadTargetKindBackupLog,
			targetName:  "my-backup",
			expectedKey: "backups/my-backup/my-backup-logs.gz",
		},
		{
			name:        "scheduled backup contents",
			targetKind:  velerov1api.DownloadTargetKindBackupContents,
			targetName:  "my-backup-20170913154901",
			expectedKey: "backups/my-backup-20170913154901/my-backup-20170913154901.tar.gz",
		},
		{
			name:        "scheduled backup log",
			targetKind:  velerov1api.DownloadTargetKindBackupLog,
			targetName:  "my-backup-20170913154901",
			expectedKey: "backups/my-backup-20170913154901/my-backup-20170913154901-logs.gz",
		},
		{
			name:        "backup contents with backup store prefix",
			targetKind:  velerov1api.DownloadTargetKindBackupContents,
			targetName:  "my-backup",
			prefix:      "velero-backups/",
			expectedKey: "velero-backups/backups/my-backup/my-backup.tar.gz",
		},
		{
			name:        "restore log",
			targetKind:  velerov1api.DownloadTargetKindRestoreLog,
			targetName:  "b-20170913154901",
			expectedKey: "restores/b-20170913154901/restore-b-20170913154901-logs.gz",
		},
		{
			name:        "restore results",
			targetKind:  velerov1api.DownloadTargetKindRestoreResults,
			targetName:  "b-20170913154901",
			expectedKey: "restores/b-20170913154901/restore-b-20170913154901-results.gz",
		},
		{
			name:        "restore results - backup has multiple dashes (e.g. restore of scheduled backup)",
			targetKind:  velerov1api.DownloadTargetKindRestoreResults,
			targetName:  "b-cool-20170913154901-20170913154902",
			expectedKey: "restores/b-cool-20170913154901-20170913154902/restore-b-cool-20170913154901-20170913154902-results.gz",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newObjectBackupStoreTestHarness("test-bucket", test.prefix)

			require.NoError(t, harness.objectStore.PutObject("test-bucket", test.expectedKey, newStringReadSeeker("foo")))

			url, err := harness.GetDownloadURL(velerov1api.DownloadTarget{Kind: test.targetKind, Name: test.targetName})
			require.NoError(t, err)
			assert.Equal(t, "a-url", url)
		})
	}
}

func encodeToBytes(obj runtime.Object) []byte {
	res, err := encode.Encode(obj, "json")
	if err != nil {
		panic(err)
	}
	return res
}

type stringReadSeeker struct {
	*strings.Reader
}

func newStringReadSeeker(s string) *stringReadSeeker {
	return &stringReadSeeker{
		Reader: strings.NewReader(s),
	}
}

func (srs *stringReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

type errorReader struct{}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, errors.New("error readers return errors")
}
