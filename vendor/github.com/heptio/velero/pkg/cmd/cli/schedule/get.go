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

package schedule

import (
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/heptio/velero/pkg/apis/velero/v1"
	"github.com/heptio/velero/pkg/client"
	"github.com/heptio/velero/pkg/cmd"
	"github.com/heptio/velero/pkg/cmd/util/output"
)

func NewGetCommand(f client.Factory, use string) *cobra.Command {
	var listOptions metav1.ListOptions

	c := &cobra.Command{
		Use:   use,
		Short: "Get schedules",
		Run: func(c *cobra.Command, args []string) {
			err := output.ValidateFlags(c)
			cmd.CheckError(err)

			veleroClient, err := f.Client()
			cmd.CheckError(err)

			var schedules *api.ScheduleList
			if len(args) > 0 {
				schedules = new(api.ScheduleList)
				for _, name := range args {
					schedule, err := veleroClient.VeleroV1().Schedules(f.Namespace()).Get(name, metav1.GetOptions{})
					cmd.CheckError(err)
					schedules.Items = append(schedules.Items, *schedule)
				}
			} else {
				schedules, err = veleroClient.VeleroV1().Schedules(f.Namespace()).List(listOptions)
				cmd.CheckError(err)
			}

			if printed, err := output.PrintWithFormat(c, schedules); printed || err != nil {
				cmd.CheckError(err)
				return
			}

			_, err = output.PrintWithFormat(c, schedules)
			cmd.CheckError(err)
		},
	}

	c.Flags().StringVarP(&listOptions.LabelSelector, "selector", "l", listOptions.LabelSelector, "only show items matching this label selector")

	output.BindFlags(c.Flags())

	return c
}
