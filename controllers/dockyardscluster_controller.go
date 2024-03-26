package controllers

import (
	"context"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha1"
	semverv3 "github.com/Masterminds/semver/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=clusters/status,verbs=patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch

type DockyardsClusterReconciler struct {
	client.Client
}

func (r *DockyardsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var dockyardsCluster dockyardsv1.Cluster
	err := r.Get(ctx, req.NamespacedName, &dockyardsCluster)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if !dockyardsCluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	var clusterAPICluster clusterv1.Cluster
	err = r.Get(ctx, client.ObjectKeyFromObject(&dockyardsCluster), &clusterAPICluster)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if apierrors.IsNotFound(err) {
		logger.Info("ignoring cluster-api cluster without dockyards cluster")

		return ctrl.Result{}, nil
	}

	matchingLabels := client.MatchingLabels{
		clusterv1.ClusterNameLabel: clusterAPICluster.Name,
	}

	var clusterAPIMachineList clusterv1.MachineList
	err = r.List(ctx, &clusterAPIMachineList, matchingLabels, client.InNamespace(clusterAPICluster.Namespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	clusterVersion := semverv3.New(3, 2, 1, "", "")
	for _, clusterAPIMachine := range clusterAPIMachineList.Items {
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

	if dockyardsCluster.Status.Version != clusterVersion.String() {
		patch := client.MergeFrom(dockyardsCluster.DeepCopy())

		dockyardsCluster.Status.Version = clusterVersion.String()

		err := r.Status().Patch(ctx, &dockyardsCluster, patch)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *DockyardsClusterReconciler) SetupWithManager(m ctrl.Manager) error {
	scheme := m.GetScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(m).For(&clusterv1.Cluster{}).Complete(r)
	if err != nil {
		return err
	}

	return nil
}
