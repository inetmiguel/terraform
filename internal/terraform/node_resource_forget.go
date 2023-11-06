// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package terraform

import (
	"log"

	"github.com/hashicorp/terraform/internal/instances"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/tfdiags"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/states"
)

// NodeForgetResourceInstance represents a resource instance that is to be
// removed from state.
type NodeForgetResourceInstance struct {
	*NodeAbstractResourceInstance
}

var (
	_ GraphNodeModuleInstance      = (*NodeForgetResourceInstance)(nil)
	_ GraphNodeConfigResource      = (*NodeForgetResourceInstance)(nil)
	_ GraphNodeResourceInstance    = (*NodeForgetResourceInstance)(nil)
	_ GraphNodeReferencer          = (*NodeForgetResourceInstance)(nil)
	_ GraphNodeExecutable          = (*NodeForgetResourceInstance)(nil)
	_ GraphNodeProviderConsumer    = (*NodeForgetResourceInstance)(nil)
	_ GraphNodeProvisionerConsumer = (*NodeForgetResourceInstance)(nil)
)

func (n *NodeForgetResourceInstance) Name() string {
	return n.ResourceInstanceAddr().String() + " (forget)"
}

func (n *NodeForgetResourceInstance) ProvidedBy() (addr addrs.ProviderConfig, exact bool) {
	if n.Addr.Resource.Resource.Mode == addrs.DataResourceMode {
		// Indicate that this node does not require a configured provider
		return nil, true
	}
	return n.NodeAbstractResourceInstance.ProvidedBy()
}

// GraphNodeExecutable
func (n *NodeForgetResourceInstance) Execute(ctx EvalContext, op walkOperation) (diags tfdiags.Diagnostics) {
	addr := n.ResourceInstanceAddr()

	is := n.instanceState
	if is == nil {
		log.Printf("[WARN] NodeForgetResourceInstance for %s with no state", addr)
	}

	var changeApply *plans.ResourceInstanceChange
	var state *states.ResourceInstanceObject

	_, providerSchema, err := getProvider(ctx, n.ResolvedProvider)
	diags = diags.Append(err)
	if diags.HasErrors() {
		return diags
	}

	changeApply, err = n.readDiff(ctx, providerSchema)
	diags = diags.Append(err)
	if changeApply == nil || diags.HasErrors() {
		return diags
	}

	state, readDiags := n.readResourceInstanceState(ctx, addr)
	diags = diags.Append(readDiags)
	if diags.HasErrors() {
		return diags
	}

	// Exit early if state is already null
	if state == nil || state.Value.IsNull() {
		return diags
	}

	diags = diags.Append(n.preApplyHook(ctx, changeApply))
	if diags.HasErrors() {
		return diags
	}

	// KEM should destroy provisioners be run ?
	// // Run destroy provisioners if not tainted
	// if state.Status != states.ObjectTainted {
	// 	applyProvisionersDiags := n.evalApplyProvisioners(ctx, state, false, configs.ProvisionerWhenDestroy)
	// 	diags = diags.Append(applyProvisionersDiags)
	// 	// keep the diags separate from the main set until we handle the cleanup

	// 	if diags.HasErrors() {
	// 		// If we have a provisioning error, then we just call
	// 		// the post-apply hook now.
	// 		diags = diags.Append(n.postApplyHook(ctx, state, diags.Err()))
	// 		return diags
	// 	}
	// }

	// Pass a nil configuration to apply
	s, d := n.apply(ctx, state, changeApply, nil, instances.RepetitionData{}, false)
	state, diags = s, diags.Append(d)
	// If there are diags, save the state first
	err = n.writeResourceInstanceState(ctx, state, workingState)
	if err != nil {
		return diags.Append(err)
	}

	diags = diags.Append(n.postApplyHook(ctx, state, diags.Err()))
	diags = diags.Append(updateStateHook(ctx))
	return diags
}
