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

package plugin

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/heptio/velero/pkg/util/test"
)

type mockRegistry struct {
	mock.Mock
}

func (r *mockRegistry) DiscoverPlugins() error {
	args := r.Called()
	return args.Error(0)
}

func (r *mockRegistry) List(kind PluginKind) []PluginIdentifier {
	args := r.Called(kind)
	return args.Get(0).([]PluginIdentifier)
}

func (r *mockRegistry) Get(kind PluginKind, name string) (PluginIdentifier, error) {
	args := r.Called(kind, name)
	var id PluginIdentifier
	if args.Get(0) != nil {
		id = args.Get(0).(PluginIdentifier)
	}
	return id, args.Error(1)
}

func TestNewManager(t *testing.T) {
	logger := test.NewLogger()
	logLevel := logrus.InfoLevel

	registry := &mockRegistry{}
	defer registry.AssertExpectations(t)

	m := NewManager(logger, logLevel, registry).(*manager)
	assert.Equal(t, logger, m.logger)
	assert.Equal(t, logLevel, m.logLevel)
	assert.Equal(t, registry, m.registry)
	assert.NotNil(t, m.restartableProcesses)
	assert.Empty(t, m.restartableProcesses)
}

type mockRestartableProcessFactory struct {
	mock.Mock
}

func (f *mockRestartableProcessFactory) newRestartableProcess(command string, logger logrus.FieldLogger, logLevel logrus.Level) (RestartableProcess, error) {
	args := f.Called(command, logger, logLevel)
	var rp RestartableProcess
	if args.Get(0) != nil {
		rp = args.Get(0).(RestartableProcess)
	}
	return rp, args.Error(1)
}

type mockRestartableProcess struct {
	mock.Mock
}

func (rp *mockRestartableProcess) addReinitializer(key kindAndName, r reinitializer) {
	rp.Called(key, r)
}

func (rp *mockRestartableProcess) reset() error {
	args := rp.Called()
	return args.Error(0)
}

func (rp *mockRestartableProcess) resetIfNeeded() error {
	args := rp.Called()
	return args.Error(0)
}

func (rp *mockRestartableProcess) getByKindAndName(key kindAndName) (interface{}, error) {
	args := rp.Called(key)
	return args.Get(0), args.Error(1)
}

func (rp *mockRestartableProcess) stop() {
	rp.Called()
}

func TestGetRestartableProcess(t *testing.T) {
	logger := test.NewLogger()
	logLevel := logrus.InfoLevel

	registry := &mockRegistry{}
	defer registry.AssertExpectations(t)

	m := NewManager(logger, logLevel, registry).(*manager)
	factory := &mockRestartableProcessFactory{}
	defer factory.AssertExpectations(t)
	m.restartableProcessFactory = factory

	// Test 1: registry error
	pluginKind := PluginKindBackupItemAction
	pluginName := "pod"
	registry.On("Get", pluginKind, pluginName).Return(nil, errors.Errorf("registry")).Once()
	rp, err := m.getRestartableProcess(pluginKind, pluginName)
	assert.Nil(t, rp)
	assert.EqualError(t, err, "registry")

	// Test 2: registry ok, factory error
	podID := PluginIdentifier{
		Command: "/command",
		Kind:    pluginKind,
		Name:    pluginName,
	}
	registry.On("Get", pluginKind, pluginName).Return(podID, nil)
	factory.On("newRestartableProcess", podID.Command, logger, logLevel).Return(nil, errors.Errorf("factory")).Once()
	rp, err = m.getRestartableProcess(pluginKind, pluginName)
	assert.Nil(t, rp)
	assert.EqualError(t, err, "factory")

	// Test 3: registry ok, factory ok
	restartableProcess := &mockRestartableProcess{}
	defer restartableProcess.AssertExpectations(t)
	factory.On("newRestartableProcess", podID.Command, logger, logLevel).Return(restartableProcess, nil).Once()
	rp, err = m.getRestartableProcess(pluginKind, pluginName)
	require.NoError(t, err)
	assert.Equal(t, restartableProcess, rp)

	// Test 4: retrieve from cache
	rp, err = m.getRestartableProcess(pluginKind, pluginName)
	require.NoError(t, err)
	assert.Equal(t, restartableProcess, rp)
}

func TestCleanupClients(t *testing.T) {
	logger := test.NewLogger()
	logLevel := logrus.InfoLevel

	registry := &mockRegistry{}
	defer registry.AssertExpectations(t)

	m := NewManager(logger, logLevel, registry).(*manager)

	for i := 0; i < 5; i++ {
		rp := &mockRestartableProcess{}
		defer rp.AssertExpectations(t)
		rp.On("stop")
		m.restartableProcesses[fmt.Sprintf("rp%d", i)] = rp
	}

	m.CleanupClients()
}

func TestGetObjectStore(t *testing.T) {
	getPluginTest(t,
		PluginKindObjectStore,
		"aws",
		func(m Manager, name string) (interface{}, error) {
			return m.GetObjectStore(name)
		},
		func(name string, sharedPluginProcess RestartableProcess) interface{} {
			return &restartableObjectStore{
				key:                 kindAndName{kind: PluginKindObjectStore, name: name},
				sharedPluginProcess: sharedPluginProcess,
			}
		},
		true,
	)
}

func TestGetBlockStore(t *testing.T) {
	getPluginTest(t,
		PluginKindBlockStore,
		"aws",
		func(m Manager, name string) (interface{}, error) {
			return m.GetBlockStore(name)
		},
		func(name string, sharedPluginProcess RestartableProcess) interface{} {
			return &restartableBlockStore{
				key:                 kindAndName{kind: PluginKindBlockStore, name: name},
				sharedPluginProcess: sharedPluginProcess,
			}
		},
		true,
	)
}

func TestGetBackupItemAction(t *testing.T) {
	getPluginTest(t,
		PluginKindBackupItemAction,
		"pod",
		func(m Manager, name string) (interface{}, error) {
			return m.GetBackupItemAction(name)
		},
		func(name string, sharedPluginProcess RestartableProcess) interface{} {
			return &restartableBackupItemAction{
				key:                 kindAndName{kind: PluginKindBackupItemAction, name: name},
				sharedPluginProcess: sharedPluginProcess,
			}
		},
		false,
	)
}

func TestGetRestoreItemAction(t *testing.T) {
	getPluginTest(t,
		PluginKindRestoreItemAction,
		"pod",
		func(m Manager, name string) (interface{}, error) {
			return m.GetRestoreItemAction(name)
		},
		func(name string, sharedPluginProcess RestartableProcess) interface{} {
			return &restartableRestoreItemAction{
				key:                 kindAndName{kind: PluginKindRestoreItemAction, name: name},
				sharedPluginProcess: sharedPluginProcess,
			}
		},
		false,
	)
}

func getPluginTest(
	t *testing.T,
	kind PluginKind,
	name string,
	getPluginFunc func(m Manager, name string) (interface{}, error),
	expectedResultFunc func(name string, sharedPluginProcess RestartableProcess) interface{},
	reinitializable bool,
) {
	logger := test.NewLogger()
	logLevel := logrus.InfoLevel

	registry := &mockRegistry{}
	defer registry.AssertExpectations(t)

	m := NewManager(logger, logLevel, registry).(*manager)
	factory := &mockRestartableProcessFactory{}
	defer factory.AssertExpectations(t)
	m.restartableProcessFactory = factory

	pluginKind := kind
	pluginName := name
	pluginID := PluginIdentifier{
		Command: "/command",
		Kind:    pluginKind,
		Name:    pluginName,
	}
	registry.On("Get", pluginKind, pluginName).Return(pluginID, nil)

	restartableProcess := &mockRestartableProcess{}
	defer restartableProcess.AssertExpectations(t)

	// Test 1: error getting restartable process
	factory.On("newRestartableProcess", pluginID.Command, logger, logLevel).Return(nil, errors.Errorf("newRestartableProcess")).Once()
	actual, err := getPluginFunc(m, pluginName)
	assert.Nil(t, actual)
	assert.EqualError(t, err, "newRestartableProcess")

	// Test 2: happy path
	factory.On("newRestartableProcess", pluginID.Command, logger, logLevel).Return(restartableProcess, nil).Once()

	expected := expectedResultFunc(name, restartableProcess)
	if reinitializable {
		key := kindAndName{kind: pluginID.Kind, name: pluginID.Name}
		restartableProcess.On("addReinitializer", key, expected)
	}

	actual, err = getPluginFunc(m, pluginName)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestGetBackupItemActions(t *testing.T) {
	tests := []struct {
		name                       string
		names                      []string
		newRestartableProcessError error
		expectedError              string
	}{
		{
			name:  "No items",
			names: []string{},
		},
		{
			name:                       "Error getting restartable process",
			names:                      []string{"a", "b", "c"},
			newRestartableProcessError: errors.Errorf("newRestartableProcess"),
			expectedError:              "newRestartableProcess",
		},
		{
			name:  "Happy path",
			names: []string{"a", "b", "c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := test.NewLogger()
			logLevel := logrus.InfoLevel

			registry := &mockRegistry{}
			defer registry.AssertExpectations(t)

			m := NewManager(logger, logLevel, registry).(*manager)
			factory := &mockRestartableProcessFactory{}
			defer factory.AssertExpectations(t)
			m.restartableProcessFactory = factory

			pluginKind := PluginKindBackupItemAction
			var pluginIDs []PluginIdentifier
			for i := range tc.names {
				pluginID := PluginIdentifier{
					Command: "/command",
					Kind:    pluginKind,
					Name:    tc.names[i],
				}
				pluginIDs = append(pluginIDs, pluginID)
			}
			registry.On("List", pluginKind).Return(pluginIDs)

			var expectedActions []interface{}
			for i := range pluginIDs {
				pluginID := pluginIDs[i]
				pluginName := pluginID.Name

				registry.On("Get", pluginKind, pluginName).Return(pluginID, nil)

				restartableProcess := &mockRestartableProcess{}
				defer restartableProcess.AssertExpectations(t)

				expected := &restartableBackupItemAction{
					key:                 kindAndName{kind: pluginKind, name: pluginName},
					sharedPluginProcess: restartableProcess,
				}

				if tc.newRestartableProcessError != nil {
					// Test 1: error getting restartable process
					factory.On("newRestartableProcess", pluginID.Command, logger, logLevel).Return(nil, errors.Errorf("newRestartableProcess")).Once()
					break
				}

				// Test 2: happy path
				if i == 0 {
					factory.On("newRestartableProcess", pluginID.Command, logger, logLevel).Return(restartableProcess, nil).Once()
				}

				expectedActions = append(expectedActions, expected)
			}

			backupItemActions, err := m.GetBackupItemActions()
			if tc.newRestartableProcessError != nil {
				assert.Nil(t, backupItemActions)
				assert.EqualError(t, err, "newRestartableProcess")
			} else {
				require.NoError(t, err)
				var actual []interface{}
				for i := range backupItemActions {
					actual = append(actual, backupItemActions[i])
				}
				assert.Equal(t, expectedActions, actual)
			}
		})
	}
}

func TestGetRestoreItemActions(t *testing.T) {
	tests := []struct {
		name                       string
		names                      []string
		newRestartableProcessError error
		expectedError              string
	}{
		{
			name:  "No items",
			names: []string{},
		},
		{
			name:                       "Error getting restartable process",
			names:                      []string{"a", "b", "c"},
			newRestartableProcessError: errors.Errorf("newRestartableProcess"),
			expectedError:              "newRestartableProcess",
		},
		{
			name:  "Happy path",
			names: []string{"a", "b", "c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := test.NewLogger()
			logLevel := logrus.InfoLevel

			registry := &mockRegistry{}
			defer registry.AssertExpectations(t)

			m := NewManager(logger, logLevel, registry).(*manager)
			factory := &mockRestartableProcessFactory{}
			defer factory.AssertExpectations(t)
			m.restartableProcessFactory = factory

			pluginKind := PluginKindRestoreItemAction
			var pluginIDs []PluginIdentifier
			for i := range tc.names {
				pluginID := PluginIdentifier{
					Command: "/command",
					Kind:    pluginKind,
					Name:    tc.names[i],
				}
				pluginIDs = append(pluginIDs, pluginID)
			}
			registry.On("List", pluginKind).Return(pluginIDs)

			var expectedActions []interface{}
			for i := range pluginIDs {
				pluginID := pluginIDs[i]
				pluginName := pluginID.Name

				registry.On("Get", pluginKind, pluginName).Return(pluginID, nil)

				restartableProcess := &mockRestartableProcess{}
				defer restartableProcess.AssertExpectations(t)

				expected := &restartableRestoreItemAction{
					key:                 kindAndName{kind: pluginKind, name: pluginName},
					sharedPluginProcess: restartableProcess,
				}

				if tc.newRestartableProcessError != nil {
					// Test 1: error getting restartable process
					factory.On("newRestartableProcess", pluginID.Command, logger, logLevel).Return(nil, errors.Errorf("newRestartableProcess")).Once()
					break
				}

				// Test 2: happy path
				if i == 0 {
					factory.On("newRestartableProcess", pluginID.Command, logger, logLevel).Return(restartableProcess, nil).Once()
				}

				expectedActions = append(expectedActions, expected)
			}

			restoreItemActions, err := m.GetRestoreItemActions()
			if tc.newRestartableProcessError != nil {
				assert.Nil(t, restoreItemActions)
				assert.EqualError(t, err, "newRestartableProcess")
			} else {
				require.NoError(t, err)
				var actual []interface{}
				for i := range restoreItemActions {
					actual = append(actual, restoreItemActions[i])
				}
				assert.Equal(t, expectedActions, actual)
			}
		})
	}
}
