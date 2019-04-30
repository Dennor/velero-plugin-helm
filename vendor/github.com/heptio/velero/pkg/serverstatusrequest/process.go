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

package serverstatusrequest

import (
	"encoding/json"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"

	velerov1api "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/buildinfo"
	velerov1client "github.com/heptio/velero/pkg/generated/clientset/versioned/typed/velero/v1"
)

const ttl = time.Minute

// Process fills out new ServerStatusRequest objects and deletes processed ones
// that have expired.
func Process(req *velerov1api.ServerStatusRequest, client velerov1client.ServerStatusRequestsGetter, clock clock.Clock, log logrus.FieldLogger) error {
	switch req.Status.Phase {
	case "", velerov1api.ServerStatusRequestPhaseNew:
		log.Info("Processing new ServerStatusRequest")
		return errors.WithStack(patch(client, req, func(req *velerov1api.ServerStatusRequest) {
			req.Status.ServerVersion = buildinfo.Version
			req.Status.ProcessedTimestamp.Time = clock.Now()
			req.Status.Phase = velerov1api.ServerStatusRequestPhaseProcessed
		}))
	case velerov1api.ServerStatusRequestPhaseProcessed:
		log.Debug("Checking whether ServerStatusRequest has expired")
		expiration := req.Status.ProcessedTimestamp.Add(ttl)
		if expiration.After(clock.Now()) {
			log.Debug("ServerStatusRequest has not expired")
			return nil
		}

		log.Debug("ServerStatusRequest has expired, deleting it")
		if err := client.ServerStatusRequests(req.Namespace).Delete(req.Name, nil); err != nil {
			return errors.WithStack(err)
		}

		return nil
	default:
		return errors.Errorf("unexpected ServerStatusRequest phase %q", req.Status.Phase)
	}
}

func patch(client velerov1client.ServerStatusRequestsGetter, req *velerov1api.ServerStatusRequest, updateFunc func(*velerov1api.ServerStatusRequest)) error {
	originalJSON, err := json.Marshal(req)
	if err != nil {
		return errors.WithStack(err)
	}

	updateFunc(req)

	updatedJSON, err := json.Marshal(req)
	if err != nil {
		return errors.WithStack(err)
	}

	patchBytes, err := jsonpatch.CreateMergePatch(originalJSON, updatedJSON)
	if err != nil {
		return errors.WithStack(err)
	}

	_, err = client.ServerStatusRequests(req.Namespace).Patch(req.Name, types.MergePatchType, patchBytes)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}
