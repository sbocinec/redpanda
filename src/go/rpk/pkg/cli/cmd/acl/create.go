// Copyright 2021 Vectorized, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package acl

import (
	"context"
	"fmt"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/twmb/types"
	"github.com/vectorizedio/redpanda/src/go/rpk/pkg/config"
	"github.com/vectorizedio/redpanda/src/go/rpk/pkg/kafka"
	"github.com/vectorizedio/redpanda/src/go/rpk/pkg/out"
)

func NewCreateCommand(fs afero.Fs) *cobra.Command {
	var a acls
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create ACLs.",

		Args: cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, _ []string) {
			p := config.ParamsFromCommand(cmd)
			cfg, err := p.Load(fs)
			out.MaybeDie(err, "unable to load config: %v", err)

			adm, err := kafka.NewAdmin(fs, p, cfg)
			out.MaybeDie(err, "unable to initialize kafka client: %v", err)
			defer adm.Close()

			b, err := a.createCreations()
			out.MaybeDieErr(err)
			results, err := adm.CreateACLs(context.Background(), b)
			out.MaybeDie(err, "unable to create ACLs: %v", err)
			if len(results) == 0 {
				fmt.Println("Specified flags created no ACLs.")
				return
			}
			types.Sort(results)

			tw := out.NewTable(headersWithError...)
			defer tw.Flush()
			for _, c := range results {
				tw.PrintStructFields(aclWithMessage{
					c.Principal,
					c.Host,
					c.Type,
					c.Name,
					c.Pattern,
					c.Operation,
					c.Permission,
					kafka.ErrMessage(c.Err),
				})
			}
		},
	}
	a.addCreateFlags(cmd)
	return cmd
}

func (a *acls) addCreateFlags(cmd *cobra.Command) {
	a.addDeprecatedFlags(cmd)

	cmd.Flags().StringSliceVar(&a.topics, topicFlag, nil, "topic to grant ACLs for (repeatable)")
	cmd.Flags().StringSliceVar(&a.groups, groupFlag, nil, "group to grant ACLs for (repeatable)")
	cmd.Flags().BoolVar(&a.cluster, clusterFlag, false, "whether to grant ACLs to the cluster")
	cmd.Flags().StringSliceVar(&a.txnIDs, txnIDFlag, nil, "transactional IDs to grant ACLs for (repeatable)")

	cmd.Flags().StringVar(&a.resourcePatternType, patternFlag, "literal", "pattern to use when matching resource names (literal or prefixed)")

	cmd.Flags().StringSliceVar(&a.operations, operationFlag, nil, "operation to grant (repeatable)")

	cmd.Flags().StringSliceVar(&a.allowPrincipals, allowPrincipalFlag, nil, "principals for which these permissions will be granted (repeatable)")
	cmd.Flags().StringSliceVar(&a.allowHosts, allowHostFlag, nil, "hosts from which access will be granted (repeatable)")
	cmd.Flags().StringSliceVar(&a.denyPrincipals, denyPrincipalFlag, nil, "principal for which these permissions will be denied (repeatable)")
	cmd.Flags().StringSliceVar(&a.denyHosts, denyHostFlag, nil, "hosts from from access will be denied (repeatable)")
}
