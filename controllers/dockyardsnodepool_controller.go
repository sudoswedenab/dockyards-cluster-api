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

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3"
	"github.com/fluxcd/pkg/runtime/patch"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=nodepools,verbs=get;list;patch;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=nodes,verbs=get;list;watch

const (
	DockyardsNodePoolFinalizer = "cluster-api.dockyards.io/finalizer"
)

type DockyardsNodePoolReconciler struct {
	client.Client
}

func (r *DockyardsNodePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	var dockyardsNodePool dockyardsv1.NodePool
	err := r.Get(ctx, req.NamespacedName, &dockyardsNodePool)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patchHelper, err := patch.NewHelper(&dockyardsNodePool, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		err := patchHelper.Patch(ctx, &dockyardsNodePool)
		if err != nil {
			result = ctrl.Result{}
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	if !dockyardsNodePool.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &dockyardsNodePool)
	}

	controllerutil.AddFinalizer(&dockyardsNodePool, DockyardsNodePoolFinalizer)

	return ctrl.Result{}, nil
}

func (r *DockyardsNodePoolReconciler) reconcileDelete(ctx context.Context, dockyardsNodePool *dockyardsv1.NodePool) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	matchingLabels := client.MatchingLabels{
		dockyardsv1.LabelNodePoolName: dockyardsNodePool.Name,
	}

	var dockyardsNodeList dockyardsv1.NodeList
	err := r.List(ctx, &dockyardsNodeList, matchingLabels, client.InNamespace(dockyardsNodePool.Namespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(dockyardsNodeList.Items) != 0 {
		logger.Info("ignoring deleted node pool with remaining nodes", "nodes", len(dockyardsNodeList.Items))

		return ctrl.Result{}, nil
	}

	controllerutil.RemoveFinalizer(dockyardsNodePool, DockyardsNodePoolFinalizer)

	return ctrl.Result{}, nil
}

func (r *DockyardsNodePoolReconciler) dockyardsNodeToDockyardsNodePool(_ context.Context, obj client.Object) []ctrl.Request {
	dockyardsNode, ok := obj.(*dockyardsv1.Node)
	if !ok {
		return nil
	}

	dockyardsNodePoolName, hasLabel := dockyardsNode.Labels[dockyardsv1.LabelNodePoolName]
	if !hasLabel {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      dockyardsNodePoolName,
				Namespace: dockyardsNode.Namespace,
			},
		},
	}
}

func (r *DockyardsNodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	scheme := mgr.GetScheme()

	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(mgr).
		For(&dockyardsv1.NodePool{}).
		Watches(
			&dockyardsv1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.dockyardsNodeToDockyardsNodePool),
		).
		Complete(r)
	if err != nil {
		return err
	}

	return nil
}
