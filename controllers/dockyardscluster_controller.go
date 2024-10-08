package controllers

import (
	"context"
	"regexp"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha2"
	semverv3 "github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=clusters/status,verbs=patch
// +kubebuilder:rbac:groups=dockyards.io,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=create;get;list;patch;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch

var (
	InvalidReasonCharacters = regexp.MustCompile("[^A-Za-z0-9_,:]")
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

		conditions.Set(&dockyardsCluster, &condition)
	} else {
		conditions.MarkFalse(&dockyardsCluster, ClusterReadyCondition, WaitingForClusterReadyConditionReason, "")
	}

	controlPlaneReadyCondition := capiconditions.Get(&cluster, clusterv1.ControlPlaneReadyCondition)
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

		conditions.Set(&dockyardsCluster, &condition)
	} else {
		conditions.MarkFalse(&dockyardsCluster, ClusterControlPlaneReadyCondition, WaitingForClusterControlPlaneReadyConditionReason, "")
	}

	matchingLabels := client.MatchingLabels{
		dockyardsv1.LabelClusterName: dockyardsCluster.Name,
	}

	var deploymentList dockyardsv1.DeploymentList
	err = r.List(ctx, &deploymentList, matchingLabels, client.InNamespace(cluster.Namespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	clusterComponentsReady := true
	for _, deployment := range deploymentList.Items {
		if !deployment.Spec.ClusterComponent {
			continue
		}

		if conditions.IsTrue(&deployment, dockyardsv1.ReadyCondition) {
			continue
		}

		conditions.MarkFalse(&dockyardsCluster, ClusterComponentsReadyCondition, WaitingForClusterComponentReadyConditionReason, "%s", deployment.Name)
		clusterComponentsReady = false

		break
	}

	if clusterComponentsReady {
		conditions.MarkTrue(&dockyardsCluster, ClusterComponentsReadyCondition, dockyardsv1.ReadyReason, "")
	}

	matchingLabels = client.MatchingLabels{
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

func (r *DockyardsClusterReconciler) dockyardsDeploymentToDockyardsCluster(_ context.Context, obj client.Object) []ctrl.Request {
	dockyardsDeployment, ok := obj.(*dockyardsv1.Deployment)
	if !ok {
		return nil
	}

	clusterName, has := dockyardsDeployment.Labels[dockyardsv1.LabelClusterName]
	if !has {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      clusterName,
				Namespace: dockyardsDeployment.Namespace,
			},
		},
	}
}

func (r *DockyardsClusterReconciler) SetupWithManager(m ctrl.Manager) error {
	scheme := m.GetScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(m).
		For(&dockyardsv1.Cluster{}).
		Owns(&clusterv1.Cluster{}).
		Watches(
			&dockyardsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.dockyardsDeploymentToDockyardsCluster),
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
	}

	conditions.SetSummary(
		dockyardsCluster,
		dockyardsv1.ReadyCondition,
		conditions.WithConditions(summaryConditions...),
	)

	return patchHelper.Patch(ctx, dockyardsCluster, opts...)
}
