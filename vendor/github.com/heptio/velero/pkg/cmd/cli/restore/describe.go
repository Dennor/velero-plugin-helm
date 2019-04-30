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

package restore

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/client"
	"github.com/heptio/velero/pkg/cmd"
	"github.com/heptio/velero/pkg/cmd/util/output"
	"github.com/heptio/velero/pkg/restic"
)

func NewDescribeCommand(f client.Factory, use string) *cobra.Command {
	var (
		listOptions metav1.ListOptions
		details     bool
	)

	c := &cobra.Command{
		Use:   use + " [NAME1] [NAME2] [NAME...]",
		Short: "Describe restores",
		Run: func(c *cobra.Command, args []string) {
			veleroClient, err := f.Client()
			cmd.CheckError(err)

			var restores *api.RestoreList
			if len(args) > 0 {
				restores = new(api.RestoreList)
				for _, name := range args {
					restore, err := veleroClient.VeleroV1().Restores(f.Namespace()).Get(name, metav1.GetOptions{})
					cmd.CheckError(err)
					restores.Items = append(restores.Items, *restore)
				}
			} else {
				restores, err = veleroClient.VeleroV1().Restores(f.Namespace()).List(listOptions)
				cmd.CheckError(err)
			}

			first := true
			for _, restore := range restores.Items {
				opts := restic.NewPodVolumeRestoreListOptions(restore.Name)
				podvolumeRestoreList, err := veleroClient.VeleroV1().PodVolumeRestores(f.Namespace()).List(opts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error getting PodVolumeRestores for restore %s: %v\n", restore.Name, err)
				}

				s := output.DescribeRestore(&restore, podvolumeRestoreList.Items, details, veleroClient)
				if first {
					first = false
					fmt.Print(s)
				} else {
					fmt.Printf("\n\n%s", s)
				}
			}
			cmd.CheckError(err)
		},
	}

	c.Flags().StringVarP(&listOptions.LabelSelector, "selector", "l", listOptions.LabelSelector, "only show items matching this label selector")
	c.Flags().BoolVar(&details, "details", details, "display additional detail in the command output")

	return c
}
