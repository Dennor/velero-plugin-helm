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

// Code generated by mockery v1.0.0. DO NOT EDIT.
package mocks

import io "io"
import mock "github.com/stretchr/testify/mock"
import time "time"

// ObjectStore is an autogenerated mock type for the ObjectStore type
type ObjectStore struct {
	mock.Mock
}

// CreateSignedURL provides a mock function with given fields: bucket, key, ttl
func (_m *ObjectStore) CreateSignedURL(bucket string, key string, ttl time.Duration) (string, error) {
	ret := _m.Called(bucket, key, ttl)

	var r0 string
	if rf, ok := ret.Get(0).(func(string, string, time.Duration) string); ok {
		r0 = rf(bucket, key, ttl)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string, time.Duration) error); ok {
		r1 = rf(bucket, key, ttl)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DeleteObject provides a mock function with given fields: bucket, key
func (_m *ObjectStore) DeleteObject(bucket string, key string) error {
	ret := _m.Called(bucket, key)

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string) error); ok {
		r0 = rf(bucket, key)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GetObject provides a mock function with given fields: bucket, key
func (_m *ObjectStore) GetObject(bucket string, key string) (io.ReadCloser, error) {
	ret := _m.Called(bucket, key)

	var r0 io.ReadCloser
	if rf, ok := ret.Get(0).(func(string, string) io.ReadCloser); ok {
		r0 = rf(bucket, key)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(io.ReadCloser)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string) error); ok {
		r1 = rf(bucket, key)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Init provides a mock function with given fields: config
func (_m *ObjectStore) Init(config map[string]string) error {
	ret := _m.Called(config)

	var r0 error
	if rf, ok := ret.Get(0).(func(map[string]string) error); ok {
		r0 = rf(config)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// ListCommonPrefixes provides a mock function with given fields: bucket, prefix, delimiter
func (_m *ObjectStore) ListCommonPrefixes(bucket string, prefix string, delimiter string) ([]string, error) {
	ret := _m.Called(bucket, prefix, delimiter)

	var r0 []string
	if rf, ok := ret.Get(0).(func(string, string, string) []string); ok {
		r0 = rf(bucket, prefix, delimiter)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]string)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string, string) error); ok {
		r1 = rf(bucket, prefix, delimiter)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ListObjects provides a mock function with given fields: bucket, prefix
func (_m *ObjectStore) ListObjects(bucket string, prefix string) ([]string, error) {
	ret := _m.Called(bucket, prefix)

	var r0 []string
	if rf, ok := ret.Get(0).(func(string, string) []string); ok {
		r0 = rf(bucket, prefix)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]string)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string) error); ok {
		r1 = rf(bucket, prefix)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// PutObject provides a mock function with given fields: bucket, key, body
func (_m *ObjectStore) PutObject(bucket string, key string, body io.Reader) error {
	ret := _m.Called(bucket, key, body)

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string, io.Reader) error); ok {
		r0 = rf(bucket, key, body)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
