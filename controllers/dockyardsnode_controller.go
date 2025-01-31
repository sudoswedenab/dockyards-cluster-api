package controllers

import (
	"cmp"
	"context"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes/status,verbs=patch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes,verbs=get;list;patch;watch

const (
	DockyardsNodeFinalizer = "cluster-api.dockyards.io/finalizer"
)

type DockyardsNodeReconciler struct {
	client.Client
}

func (r *DockyardsNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	var dockyardsNode dockyardsv1.Node
	err := r.Get(ctx, req.NamespacedName, &dockyardsNode)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !util.HasOwner(dockyardsNode.OwnerReferences, clusterv1.GroupVersion.String(), []string{"Machine"}) {
		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(&dockyardsNode, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		summaryConditions := []string{
			MachineReadyCondition,
			string(clusterv1.MachineNodeHealthyCondition),
		}

		conditions.SetSummary(
			&dockyardsNode,
			dockyardsv1.ReadyCondition,
			conditions.WithConditions(summaryConditions...),
		)

		err := patchHelper.Patch(ctx, &dockyardsNode)
		if err != nil {
			result = ctrl.Result{}
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	ownerMachine, err := util.GetOwnerMachine(ctx, r.Client, dockyardsNode.ObjectMeta)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if apierrors.IsNotFound(err) {
		if !dockyardsNode.DeletionTimestamp.IsZero() {
			controllerutil.RemoveFinalizer(&dockyardsNode, DockyardsNodeFinalizer)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if dockyardsNode.DeletionTimestamp.IsZero() && controllerutil.AddFinalizer(&dockyardsNode, DockyardsNodeFinalizer) {
		return ctrl.Result{}, nil
	}

	readyCondition := capiconditions.Get(ownerMachine, clusterv1.ReadyCondition)
	if readyCondition != nil {
		condition := metav1.Condition{
			Type:               MachineReadyCondition,
			Reason:             cmp.Or(readyCondition.Reason, dockyardsv1.ReadyReason),
			Message:            readyCondition.Message,
			LastTransitionTime: readyCondition.LastTransitionTime,
			Status:             metav1.ConditionStatus(readyCondition.Status),
		}

		conditions.Set(&dockyardsNode, &condition)
	} else {
		conditions.MarkFalse(&dockyardsNode, MachineReadyCondition, WaitingForMachineReadyConditionReason, "")
	}

	machineNodeHealthyCondition := capiconditions.Get(ownerMachine, clusterv1.MachineNodeHealthyCondition)
	if machineNodeHealthyCondition != nil {
		condition := metav1.Condition{
			Type:               string(clusterv1.MachineNodeHealthyCondition),
			Reason:             cmp.Or(machineNodeHealthyCondition.Reason, dockyardsv1.ReadyReason),
			Message:            machineNodeHealthyCondition.Message,
			LastTransitionTime: machineNodeHealthyCondition.LastTransitionTime,
			Status:             metav1.ConditionStatus(machineNodeHealthyCondition.Status),
		}

		conditions.Set(&dockyardsNode, &condition)
	} else {
		conditions.MarkFalse(&dockyardsNode, string(clusterv1.MachineNodeHealthyCondition), WaitingForNodeHealthyConditionReason, "")
	}

	return ctrl.Result{}, nil
}

func (r *DockyardsNodeReconciler) machineToDockyardsNode(ctx context.Context, obj client.Object) []ctrl.Request {
	machine, ok := obj.(*clusterv1.Machine)
	if !ok {
		return nil
	}

	matchingLabels := client.MatchingLabels{
		MachineNameLabel: machine.Name,
	}

	var dockyardsNodeList dockyardsv1.NodeList
	err := r.List(ctx, &dockyardsNodeList, matchingLabels, client.InNamespace(machine.Namespace))
	if err != nil {
		panic(err)
	}

	requests := []ctrl.Request{}

	for _, dockyardsNode := range dockyardsNodeList.Items {
		requests = append(requests, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      dockyardsNode.Name,
				Namespace: dockyardsNode.Namespace,
			},
		})
	}

	return requests
}

func (r *DockyardsNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	scheme := mgr.GetScheme()

	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(mgr).
		For(&dockyardsv1.Node{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToDockyardsNode),
		).
		Complete(r)
	if err != nil {
		return err
	}

	return nil
}
