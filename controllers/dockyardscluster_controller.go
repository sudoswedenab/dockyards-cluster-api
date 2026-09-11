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
	"regexp"

	semverv3 "github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	controlplanev1 "github.com/siderolabs/cluster-api-control-plane-provider-talos/api/v1alpha3"
	dockyardsv1 "github.com/sudoswedenab/dockyards-backend/api/v1alpha3"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	providerv1 "sigs.k8s.io/cluster-api-provider-kubevirt/api/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	capiv1beta1conditions "sigs.k8s.io/cluster-api/util/conditions/deprecated/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=create;get;list;patch;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=taloscontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=clusters/status,verbs=patch
// +kubebuilder:rbac:groups=dockyards.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=workloads,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kubevirtclusters,verbs=get;list;watch

var InvalidReasonCharacters = regexp.MustCompile("[^A-Za-z0-9_,:]")

const (
	KubevirtClusterKind   = "KubevirtCluster"
	TalosControlPlaneKind = "TalosControlPlane"
)

type DockyardsClusterReconciler struct {
	client.Client
}

func (r *DockyardsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	logger := ctrl.LoggerFrom(ctx)

	var dockyardsCluster dockyardsv1.Cluster
	err := r.Get(ctx, req.NamespacedName, &dockyardsCluster)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !dockyardsCluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(&dockyardsCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		err := patchDockyardsCluster(ctx, &dockyardsCluster, patchHelper)
		if err != nil {
			result = ctrl.Result{}
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	kubevirtCluster := providerv1.KubevirtCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dockyardsCluster.Name,
			Namespace: dockyardsCluster.Namespace,
		},
	}
	err = r.Get(ctx, req.NamespacedName, &kubevirtCluster)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	talosControlPlane := controlplanev1.TalosControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dockyardsCluster.Name,
			Namespace: dockyardsCluster.Namespace,
		},
	}
	err = r.Get(ctx, req.NamespacedName, &talosControlPlane)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cluster := clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dockyardsCluster.Name,
			Namespace: dockyardsCluster.Namespace,
		},
	}

	operationResult, err := controllerutil.CreateOrPatch(ctx, r.Client, &cluster, func() error {
		controller := true

		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}

		cluster.Labels[dockyardsv1.LabelClusterName] = dockyardsCluster.Name
		cluster.Labels[dockyardsv1.LabelOrganizationName] = dockyardsCluster.Labels[dockyardsv1.LabelOrganizationName]

		cluster.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         dockyardsv1.GroupVersion.String(),
				Kind:               dockyardsv1.ClusterKind,
				Name:               dockyardsCluster.Name,
				UID:                dockyardsCluster.UID,
				Controller:         &controller,
				BlockOwnerDeletion: &controller,
			},
		}

		cluster.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{
			APIGroup: providerv1.GroupVersion.Group,
			Kind:     KubevirtClusterKind,
			Name:     kubevirtCluster.Name,
		}

		cluster.Spec.ControlPlaneRef = clusterv1.ContractVersionedObjectReference{
			APIGroup: controlplanev1.GroupVersion.Group,
			Kind:     TalosControlPlaneKind,
			Name:     talosControlPlane.Name,
		}

		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	if operationResult != controllerutil.OperationResultNone {
		logger.Info("reconciled cluster-api cluster", "result", operationResult)
	}

	result, err = r.reconcileConditions(ctx, &dockyardsCluster, &cluster)
	if err != nil {
		return result, err
	}

	matchingLabels := client.MatchingLabels{
		clusterv1.ClusterNameLabel: cluster.Name,
	}

	var machineList clusterv1.MachineList
	err = r.List(ctx, &machineList, matchingLabels, client.InNamespace(cluster.Namespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	clusterVersion := semverv3.New(3, 2, 1, "", "")
	for _, clusterAPIMachine := range machineList.Items {
		if clusterAPIMachine.Spec.Version == "" {
			continue
		}

		machineVersion, err := semverv3.NewVersion(clusterAPIMachine.Spec.Version)
		if err != nil {
			continue
		}

		if clusterVersion.GreaterThan(machineVersion) {
			clusterVersion = machineVersion
		}
	}

	dockyardsCluster.Status.Version = clusterVersion.Original()

	return ctrl.Result{}, nil
}

func (r *DockyardsClusterReconciler) reconcileConditions(ctx context.Context, dockyardsCluster *dockyardsv1.Cluster, cluster *clusterv1.Cluster) (ctrl.Result, error) {
	matchingLabels := client.MatchingLabels{
		dockyardsv1.LabelClusterName: dockyardsCluster.Name,
	}

	var workloadList dockyardsv1.WorkloadList
	err := r.List(ctx, &workloadList, matchingLabels, client.InNamespace(cluster.Namespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	clusterComponentsReady := true
	for _, workload := range workloadList.Items {
		if !workload.Spec.ClusterComponent {
			continue
		}

		if conditions.IsTrue(&workload, dockyardsv1.ReadyCondition) {
			continue
		}

		conditions.MarkFalse(dockyardsCluster, ClusterComponentsReadyCondition, WaitingForClusterComponentReadyConditionReason, "%s", workload.Name)
		clusterComponentsReady = false

		break
	}

	if clusterComponentsReady {
		conditions.MarkTrue(dockyardsCluster, ClusterComponentsReadyCondition, dockyardsv1.ReadyReason, "")
	}

	availableCondition := meta.FindStatusCondition(cluster.Status.Conditions, clusterv1.ClusterAvailableCondition)
	if availableCondition != nil {
		condition := metav1.Condition{
			Type:               ClusterAvailableCondition,
			Status:             availableCondition.Status,
			Reason:             availableCondition.Reason,
			Message:            availableCondition.Message,
			LastTransitionTime: availableCondition.LastTransitionTime,
		}

		conditions.Set(dockyardsCluster, &condition)

		conditions.Delete(dockyardsCluster, ClusterReadyCondition)
		conditions.Delete(dockyardsCluster, ClusterControlPlaneReadyCondition)

		return ctrl.Result{}, nil
	}

	legacyClusterReadyCondition := capiv1beta1conditions.Get(cluster, clusterv1.ReadyV1Beta1Condition)
	legacyControlPlaneReadyCondition := capiv1beta1conditions.Get(cluster, clusterv1.ControlPlaneReadyV1Beta1Condition)
	if legacyClusterReadyCondition != nil || legacyControlPlaneReadyCondition != nil {
		conditions.Delete(dockyardsCluster, ClusterAvailableCondition)

		if legacyClusterReadyCondition != nil {
			condition := metav1.Condition{
				Type:               ClusterReadyCondition,
				Status:             metav1.ConditionStatus(legacyClusterReadyCondition.Status),
				Reason:             legacyClusterReadyCondition.Reason,
				Message:            legacyClusterReadyCondition.Message,
				LastTransitionTime: legacyClusterReadyCondition.LastTransitionTime,
			}

			if InvalidReasonCharacters.FindString(condition.Reason) != "" {
				condition.Message = condition.Reason
				condition.Reason = WaitingForClusterFallbackReason
			}

			if condition.Status == metav1.ConditionTrue && condition.Reason == "" {
				condition.Reason = dockyardsv1.ReadyReason
			}

			conditions.Set(dockyardsCluster, &condition)
		} else {
			conditions.MarkFalse(dockyardsCluster, ClusterReadyCondition, WaitingForClusterReadyConditionReason, "")
		}

		if legacyControlPlaneReadyCondition != nil {
			condition := metav1.Condition{
				Type:               ClusterControlPlaneReadyCondition,
				Status:             metav1.ConditionStatus(legacyControlPlaneReadyCondition.Status),
				Reason:             legacyControlPlaneReadyCondition.Reason,
				Message:            legacyControlPlaneReadyCondition.Message,
				LastTransitionTime: legacyControlPlaneReadyCondition.LastTransitionTime,
			}

			if InvalidReasonCharacters.FindString(condition.Reason) != "" {
				condition.Message = condition.Reason
				condition.Reason = WaitingForClusterControlPlaneFallbackReason
			}

			if condition.Status == metav1.ConditionTrue && condition.Reason == "" {
				condition.Reason = dockyardsv1.ReadyReason
			}

			conditions.Set(dockyardsCluster, &condition)
		} else {
			conditions.MarkFalse(dockyardsCluster, ClusterControlPlaneReadyCondition, WaitingForClusterControlPlaneReadyConditionReason, "")
		}

		return ctrl.Result{}, nil
	}

	clusterReadyCondition := capiconditions.Get(cluster, clusterv1.ReadyCondition)
	if clusterReadyCondition != nil {
		condition := metav1.Condition{
			Type:               ClusterReadyCondition,
			Status:             metav1.ConditionStatus(clusterReadyCondition.Status),
			Reason:             clusterReadyCondition.Reason,
			Message:            clusterReadyCondition.Message,
			LastTransitionTime: clusterReadyCondition.LastTransitionTime,
		}

		if InvalidReasonCharacters.FindString(condition.Reason) != "" {
			condition.Message = condition.Reason
			condition.Reason = WaitingForClusterFallbackReason
		}

		if condition.Status == metav1.ConditionTrue && condition.Reason == "" {
			condition.Reason = dockyardsv1.ReadyReason
		}

		conditions.Set(dockyardsCluster, &condition)
	} else {
		conditions.MarkFalse(dockyardsCluster, ClusterReadyCondition, WaitingForClusterReadyConditionReason, "")
	}

	controlPlaneReadyCondition := capiconditions.Get(cluster, clusterv1.ClusterControlPlaneAvailableCondition)
	if controlPlaneReadyCondition != nil {
		condition := metav1.Condition{
			Type:               ClusterControlPlaneReadyCondition,
			Status:             metav1.ConditionStatus(controlPlaneReadyCondition.Status),
			Reason:             controlPlaneReadyCondition.Reason,
			Message:            controlPlaneReadyCondition.Message,
			LastTransitionTime: controlPlaneReadyCondition.LastTransitionTime,
		}

		if InvalidReasonCharacters.FindString(condition.Reason) != "" {
			condition.Message = condition.Reason
			condition.Reason = WaitingForClusterControlPlaneFallbackReason
		}

		if condition.Status == metav1.ConditionTrue && condition.Reason == "" {
			condition.Reason = dockyardsv1.ReadyReason
		}

		conditions.Set(dockyardsCluster, &condition)
	} else {
		conditions.MarkFalse(dockyardsCluster, ClusterControlPlaneReadyCondition, WaitingForClusterControlPlaneReadyConditionReason, "")
	}

	return ctrl.Result{}, nil
}

func (r *DockyardsClusterReconciler) dockyardsWorkloadToDockyardsCluster(_ context.Context, obj client.Object) []ctrl.Request {
	dockyardsWorkload, ok := obj.(*dockyardsv1.Workload)
	if !ok {
		return nil
	}

	clusterName, has := dockyardsWorkload.Labels[dockyardsv1.LabelClusterName]
	if !has {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      clusterName,
				Namespace: dockyardsWorkload.Namespace,
			},
		},
	}
}

func (r *DockyardsClusterReconciler) talosControlPlaneToDockyardsCluster(_ context.Context, obj client.Object) []ctrl.Request {
	tcp, ok := obj.(*controlplanev1.TalosControlPlane)
	if !ok {
		return nil
	}

	clusterName, has := tcp.Labels[dockyardsv1.LabelClusterName]
	if !has || clusterName == "" {
		return nil
	}

	return []ctrl.Request{{
		NamespacedName: types.NamespacedName{
			Name:      clusterName,
			Namespace: tcp.Namespace,
		},
	}}
}

func (r *DockyardsClusterReconciler) kubevirtClusterToDockyardsCluster(_ context.Context, obj client.Object) []ctrl.Request {
	kv, ok := obj.(*providerv1.KubevirtCluster)
	if !ok {
		return nil
	}

	clusterName, has := kv.Labels[dockyardsv1.LabelClusterName]
	if !has || clusterName == "" {
		return nil
	}

	return []ctrl.Request{{
		NamespacedName: types.NamespacedName{
			Name:      clusterName,
			Namespace: kv.Namespace,
		},
	}}
}

func (r *DockyardsClusterReconciler) SetupWithManager(m ctrl.Manager) error {
	scheme := m.GetScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)
	_ = providerv1.AddToScheme(scheme)
	_ = controlplanev1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(m).
		For(&dockyardsv1.Cluster{}).
		Owns(&clusterv1.Cluster{}).
		Watches(
			&dockyardsv1.Workload{},
			handler.EnqueueRequestsFromMapFunc(r.dockyardsWorkloadToDockyardsCluster),
		).
		Watches(
			&controlplanev1.TalosControlPlane{},
			handler.EnqueueRequestsFromMapFunc(r.talosControlPlaneToDockyardsCluster),
		).
		Watches(
			&providerv1.KubevirtCluster{},
			handler.EnqueueRequestsFromMapFunc(r.kubevirtClusterToDockyardsCluster),
		).
		Complete(r)
	if err != nil {
		return err
	}

	return nil
}

func patchDockyardsCluster(ctx context.Context, dockyardsCluster *dockyardsv1.Cluster, patchHelper *patch.Helper, opts ...patch.Option) error {
	summaryConditions := []string{
		ClusterReadyCondition,
		ClusterControlPlaneReadyCondition,
		ClusterComponentsReadyCondition,
		ClusterAvailableCondition,
	}

	conditions.SetSummary(
		dockyardsCluster,
		dockyardsv1.ReadyCondition,
		conditions.WithConditions(summaryConditions...),
	)

	return patchHelper.Patch(ctx, dockyardsCluster, opts...)
}
