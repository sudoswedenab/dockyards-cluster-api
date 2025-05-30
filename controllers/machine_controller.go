// Copyright 2025 Sudo Sweden AB
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"context"

	dockyardsv1 "github.com/sudoswedenab/dockyards-backend/api/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=nodepools,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes,verbs=create;delete;get;list;patch;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes/status,verbs=patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;patch;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch

type MachineReconciler struct {
	client.Client
}

func (r *MachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	logger := ctrl.LoggerFrom(ctx)

	var machine clusterv1.Machine
	err := r.Get(ctx, req.NamespacedName, &machine)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if machine.Spec.ClusterName == "" {
		logger.Info("ignoring machine with empty cluster name")

		return ctrl.Result{}, nil
	}

	if machine.Spec.InfrastructureRef.Name == "" {
		logger.Info("ignoring machine with empty infrastructure ref")

		return ctrl.Result{}, nil
	}

	result, err = r.reconcileDockyardsNode(ctx, &machine)
	if err != nil {
		return result, err
	}

	return ctrl.Result{}, nil
}

func (r *MachineReconciler) reconcileDockyardsNode(ctx context.Context, machine *clusterv1.Machine) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var dockyardsNodePoolName string

	if util.IsControlPlaneMachine(machine) {
		var cluster clusterv1.Cluster
		err := r.Get(ctx, client.ObjectKey{Name: machine.Spec.ClusterName, Namespace: machine.Namespace}, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}

		if cluster.Spec.ControlPlaneRef == nil {
			logger.Info("ignoring machine with empty cluster control plane reference")

			return ctrl.Result{}, nil
		}

		dockyardsNodePoolName = cluster.Spec.ControlPlaneRef.Name
	}

	machineDeploymentName, hasLabel := machine.Labels[clusterv1.MachineDeploymentNameLabel]
	if hasLabel {
		dockyardsNodePoolName = machineDeploymentName
	}

	if dockyardsNodePoolName == "" {
		logger.Info("unable to find dockyards node pool from machine")

		return ctrl.Result{}, nil
	}

	var dockyardsNodePool dockyardsv1.NodePool
	err := r.Get(ctx, client.ObjectKey{Name: dockyardsNodePoolName, Namespace: machine.Namespace}, &dockyardsNodePool)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	dockyardsNode := dockyardsv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machine.Spec.InfrastructureRef.Name,
			Namespace: machine.Namespace,
		},
	}

	operationResult, err := controllerutil.CreateOrPatch(ctx, r.Client, &dockyardsNode, func() error {
		dockyardsNode.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       machine.Name,
				UID:        machine.UID,
			},
		}

		if dockyardsNode.Labels == nil {
			dockyardsNode.Labels = make(map[string]string)
		}

		dockyardsNode.Labels[dockyardsv1.LabelNodePoolName] = dockyardsNodePool.Name
		dockyardsNode.Labels[dockyardsv1.LabelClusterName] = machine.Spec.ClusterName
		dockyardsNode.Labels[MachineNameLabel] = machine.Name

		if machine.Spec.ProviderID != nil {
			dockyardsNode.Spec.ProviderID = machine.Spec.ProviderID
		}

		if machine.Status.NodeInfo != nil {
			dockyardsNode.Status.SystemInfo = machine.Status.NodeInfo
		}

		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	if operationResult == controllerutil.OperationResultCreated {
		return ctrl.Result{Requeue: true}, nil
	}

	if operationResult != controllerutil.OperationResultNone {
		logger.Info("reconciled dockyards node", "result", operationResult)
	}

	return ctrl.Result{}, nil
}

func (r *MachineReconciler) SetupWithManager(m ctrl.Manager) error {
	scheme := m.GetScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(m).For(&clusterv1.Machine{}).Complete(r)
	if err != nil {
		return err
	}

	return nil
}
