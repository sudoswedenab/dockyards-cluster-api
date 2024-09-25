package controllers

import (
	"cmp"
	"context"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha2"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=nodepools,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes,verbs=create;delete;get;list;patch;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes/status,verbs=patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;patch;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch

const (
	ClusterAPIMachineFinalizer = "clusterapi.dockyards.io/finalizer"
)

type ClusterAPIMachineReconciler struct {
	client.Client
}

func (r *ClusterAPIMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
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

	patchHelper, err := patch.NewHelper(&machine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		err := patchHelper.Patch(ctx, &machine)
		if err != nil {
			result = ctrl.Result{}
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	if !machine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &machine)
	}

	if !controllerutil.ContainsFinalizer(&machine, ClusterAPIMachineFinalizer) {
		controllerutil.AddFinalizer(&machine, ClusterAPIMachineFinalizer)

		return ctrl.Result{}, nil
	}

	var dockyardsNodePoolName string

	if util.IsControlPlaneMachine(&machine) {
		var clusterAPICluster clusterv1.Cluster
		err := r.Get(ctx, client.ObjectKey{Name: machine.Spec.ClusterName, Namespace: machine.Namespace}, &clusterAPICluster)
		if err != nil {
			return ctrl.Result{}, err
		}

		if clusterAPICluster.Spec.ControlPlaneRef == nil {
			logger.Info("ignoring machine with empty cluster control plane reference")

			return ctrl.Result{}, nil
		}

		dockyardsNodePoolName = clusterAPICluster.Spec.ControlPlaneRef.Name
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
	err = r.Get(ctx, client.ObjectKey{Name: dockyardsNodePoolName, Namespace: machine.Namespace}, &dockyardsNodePool)
	if err != nil {
		return ctrl.Result{}, err
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
				APIVersion: dockyardsv1.GroupVersion.String(),
				Kind:       dockyardsv1.NodePoolKind,
				Name:       dockyardsNodePool.Name,
				UID:        dockyardsNodePool.UID,
			},
		}

		if dockyardsNode.Labels == nil {
			dockyardsNode.Labels = make(map[string]string)
		}

		dockyardsNode.Labels[dockyardsv1.LabelNodePoolName] = dockyardsNodePool.Name

		if machine.Spec.ProviderID != nil {
			dockyardsNode.Status.CloudServiceID = *machine.Spec.ProviderID
		}

		if machine.Status.NodeInfo != nil {
			dockyardsNode.Status.SystemInfo = machine.Status.NodeInfo
		}

		readyCondition := capiconditions.Get(&machine, clusterv1.ReadyCondition)
		if readyCondition != nil {
			condition := metav1.Condition{
				Type:               MachineReadyCondition,
				Reason:             cmp.Or(readyCondition.Reason, ReadyReasonNotRequiredReason),
				Message:            readyCondition.Message,
				LastTransitionTime: readyCondition.LastTransitionTime,
				Status:             metav1.ConditionStatus(readyCondition.Status),
			}

			conditions.Set(&dockyardsNode, &condition)
		} else {
			conditions.MarkFalse(&dockyardsNode, MachineReadyCondition, WaitingForMachineReadyConditionReason, "")
		}

		machineNodeHealthyCondition := capiconditions.Get(&machine, clusterv1.MachineNodeHealthyCondition)
		if machineNodeHealthyCondition != nil {
			condition := metav1.Condition{
				Type:               string(clusterv1.MachineNodeHealthyCondition),
				Reason:             cmp.Or(machineNodeHealthyCondition.Reason, ReadyReasonNotRequiredReason),
				Message:            machineNodeHealthyCondition.Message,
				LastTransitionTime: machineNodeHealthyCondition.LastTransitionTime,
				Status:             metav1.ConditionStatus(machineNodeHealthyCondition.Status),
			}

			conditions.Set(&dockyardsNode, &condition)
		} else {
			conditions.MarkFalse(&dockyardsNode, string(clusterv1.MachineNodeHealthyCondition), WaitingForNodeHealthyConditionReason, "")
		}

		summaryConditions := []string{
			MachineReadyCondition,
			string(clusterv1.MachineNodeHealthyCondition),
		}

		conditions.SetSummary(
			&dockyardsNode,
			dockyardsv1.ReadyCondition,
			conditions.WithConditions(summaryConditions...),
		)

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

func (r *ClusterAPIMachineReconciler) reconcileDelete(ctx context.Context, machine *clusterv1.Machine) (ctrl.Result, error) {
	if machine.Status.NodeRef != nil {
		dockyardsNode := dockyardsv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      machine.Status.NodeRef.Name,
				Namespace: machine.Namespace,
			},
		}

		err := r.Delete(ctx, &dockyardsNode)
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}

	if controllerutil.ContainsFinalizer(machine, ClusterAPIMachineFinalizer) {
		controllerutil.RemoveFinalizer(machine, ClusterAPIMachineFinalizer)
	}

	return ctrl.Result{}, nil
}

func (r *ClusterAPIMachineReconciler) SetupWithManager(m ctrl.Manager) error {
	scheme := m.GetScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(m).For(&clusterv1.Machine{}).Complete(r)
	if err != nil {
		return err
	}

	return nil
}
