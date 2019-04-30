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
	"fmt"
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/printers"

	v1 "github.com/heptio/velero/pkg/apis/velero/v1"
)

var (
	restoreColumns = []string{"NAME", "BACKUP", "STATUS", "WARNINGS", "ERRORS", "CREATED", "SELECTOR"}
)

func printRestoreList(list *v1.RestoreList, w io.Writer, options printers.PrintOptions) error {
	for i := range list.Items {
		if err := printRestore(&list.Items[i], w, options); err != nil {
			return err
		}
	}
	return nil
}

func printRestore(restore *v1.Restore, w io.Writer, options printers.PrintOptions) error {
	name := printers.FormatResourceName(options.Kind, restore.Name, options.WithKind)

	if options.WithNamespace {
		if _, err := fmt.Fprintf(w, "%s\t", restore.Namespace); err != nil {
			return err
		}
	}

	status := restore.Status.Phase
	if status == "" {
		status = v1.RestorePhaseNew
	}

	if _, err := fmt.Fprintf(
		w,
		"%s\t%s\t%s\t%d\t%d\t%s\t%s",
		name,
		restore.Spec.BackupName,
		status,
		restore.Status.Warnings,
		restore.Status.Errors,
		restore.CreationTimestamp.Time,
		metav1.FormatLabelSelector(restore.Spec.LabelSelector),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, printers.AppendLabels(restore.Labels, options.ColumnLabels)); err != nil {
		return err
	}

	_, err := fmt.Fprint(w, printers.AppendAllLabels(options.ShowLabels, restore.Labels))
	return err
}
