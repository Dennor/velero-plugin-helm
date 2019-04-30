/*
Copyright 2017, 2019 the Velero contributors.

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

package main

import (
	"github.com/heptio/velero/pkg/client"
	veleroplugin "github.com/heptio/velero/pkg/plugin"
	"github.com/sirupsen/logrus"
)

func main() {
	f := client.NewFactory("velero-plugin-helm")
	srv := veleroplugin.NewServer(veleroplugin.NewLogger())
	for _, resource := range []string{"configmaps", "secrets"} {
		srv.RegisterBackupItemAction("velero-plugin-helm/"+resource, newBackupPlugin(f, resource))
	}
	srv.Serve()
}

func newBackupPlugin(f client.Factory, resource string) func(logrus.FieldLogger) (interface{}, error) {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		clientset, err := f.KubeClient()
		if err != nil {
			return nil, err
		}
		var sf storageFactory
		switch resource {
		case "configmaps":
			sf = &configmapsStorage{clientset}
		case "secrets":
			sf = &secretsStorage{clientset}
		}
		return &BackupPlugin{storage: sf, log: logger}, nil
	}
}
