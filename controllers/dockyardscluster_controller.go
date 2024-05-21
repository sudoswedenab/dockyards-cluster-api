package controllers

import (
	"cmp"
	"context"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha1"
	semverv3 "github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=clusters/status,verbs=patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=create;get;list;patch;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch

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

	cluster := clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dockyardsCluster.Name,
			Namespace: dockyardsCluster.Namespace,
		},
	}

	operationResult, err := controllerutil.CreateOrPatch(ctx, r.Client, &cluster, func() error {
		controller := true

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

		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	if operationResult != controllerutil.OperationResultNone {
		logger.Info("reconciled cluster-api cluster", "result", operationResult)
	}

	clusterReadyCondition := capiconditions.Get(&cluster, clusterv1.ReadyCondition)
	if clusterReadyCondition != nil {
		condition := metav1.Condition{
			Type:               ClusterReadyCondition,
			Status:             metav1.ConditionStatus(clusterReadyCondition.Status),
			Reason:             cmp.Or(clusterReadyCondition.Reason, NoReasonReason),
			Message:            clusterReadyCondition.Message,
			LastTransitionTime: clusterReadyCondition.LastTransitionTime,
		}

		conditions.Set(&dockyardsCluster, &condition)
	} else {
		conditions.MarkFalse(&dockyardsCluster, ClusterReadyCondition, WaitingForClusterReadyConditionReason, "")
	}

	controlPlaneReadyCondition := capiconditions.Get(&cluster, clusterv1.ControlPlaneReadyCondition)
	if controlPlaneReadyCondition != nil {
		condition := metav1.Condition{
			Type:               ClusterControlPlaneReadyCondition,
			Status:             metav1.ConditionStatus(controlPlaneReadyCondition.Status),
			Reason:             cmp.Or(controlPlaneReadyCondition.Reason, NoReasonReason),
			Message:            controlPlaneReadyCondition.Message,
			LastTransitionTime: controlPlaneReadyCondition.LastTransitionTime,
		}

		conditions.Set(&dockyardsCluster, &condition)
	} else {
		conditions.MarkFalse(&dockyardsCluster, ClusterControlPlaneReadyCondition, WaitingForClusterControlPlaneReadyConditionReason, "")
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
		if clusterAPIMachine.Spec.Version == nil {
			continue
		}

		machineVersion, err := semverv3.NewVersion(*clusterAPIMachine.Spec.Version)
		if err != nil {
			logger.Error(err, "error parsing version as semver")

			continue
		}

		if clusterVersion.GreaterThan(machineVersion) {
			clusterVersion = machineVersion
		}
	}

	dockyardsCluster.Status.Version = clusterVersion.Original()

	return ctrl.Result{}, nil
}

func (r *DockyardsClusterReconciler) SetupWithManager(m ctrl.Manager) error {
	scheme := m.GetScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(m).
		For(&dockyardsv1.Cluster{}).
		Owns(&clusterv1.Cluster{}).
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
	}

	conditions.SetSummary(
		dockyardsCluster,
		dockyardsv1.ReadyCondition,
		conditions.WithConditions(summaryConditions...),
	)

	return patchHelper.Patch(ctx, dockyardsCluster, opts...)
}
