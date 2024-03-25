package controllers

import (
	"context"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ClusterAPIMachineReadyCondition = "ClusterAPIMachineReady"
)

type ClusterAPIMachineReconciler struct {
	client.Client
}

func (r *ClusterAPIMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	if machine.Status.NodeRef == nil {
		logger.Info("ignoring machine with empty node reference")

		return ctrl.Result{}, nil
	}

	var infrastructure unstructured.Unstructured
	infrastructure.SetAPIVersion(machine.Spec.InfrastructureRef.APIVersion)
	infrastructure.SetKind(machine.Spec.InfrastructureRef.Kind)

	err = r.Get(ctx, client.ObjectKey{Name: machine.Spec.InfrastructureRef.Name, Namespace: machine.Namespace}, &infrastructure)
	if err != nil {
		return ctrl.Result{}, err
	}

	annotations := infrastructure.GetAnnotations()
	clonedFromName, hasAnnotation := annotations[clusterv1.TemplateClonedFromNameAnnotation]
	if !hasAnnotation {
		logger.Info("ignoring machine with missing infrastructure annotation")

		return ctrl.Result{}, nil
	}

	var dockyardsNodePool dockyardsv1.NodePool
	err = r.Get(ctx, client.ObjectKey{Name: clonedFromName, Namespace: machine.Namespace}, &dockyardsNodePool)
	if err != nil {
		return ctrl.Result{}, err
	}

	dockyardsNode := dockyardsv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machine.Name,
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

		dockyardsNode.Labels[dockyardsv1.NodePoolNameLabel] = dockyardsNodePool.Name

		if machine.Spec.ProviderID != nil {
			dockyardsNode.Status.CloudServiceID = *machine.Spec.ProviderID
		}

		readyCondition := capiconditions.Get(&machine, clusterv1.ReadyCondition)
		if readyCondition != nil {
			condition := metav1.Condition{
				Type:               dockyardsv1.ReadyCondition,
				Reason:             readyCondition.Reason,
				Message:            readyCondition.Message,
				LastTransitionTime: readyCondition.LastTransitionTime,
				Status:             metav1.ConditionStatus(readyCondition.Status),
			}

			if readyCondition.Reason == "" {
				condition.Reason = "ReadyReasonNotNeeded"
			}

			changed := meta.SetStatusCondition(&dockyardsNode.Status.Conditions, condition)
			logger.Info("set status condition", "changed", changed)
		}

		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	if operationResult != controllerutil.OperationResultNone {
		logger.Info("reconciled dockyards node", "result", operationResult)
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
