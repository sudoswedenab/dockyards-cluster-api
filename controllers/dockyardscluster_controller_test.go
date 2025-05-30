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
	"testing"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3"
	"bitbucket.org/sudosweden/dockyards-cluster-api/test/mockcrds"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestDockyardsClusterController_Reconcile(t *testing.T) {
	env := envtest.Environment{
		CRDs: mockcrds.CRDs,
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	t.Cleanup(func() {
		cancel()
		env.Stop()
	})

	scheme := runtime.NewScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
		},
	}

	err = c.Create(ctx, &namespace)
	if err != nil {
		t.Fatal(err)
	}

	r := DockyardsClusterReconciler{
		Client: c,
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime"),
	}

	t.Run("test v1beta2 conditions", func(t *testing.T) {
		dockyardsCluster := dockyardsv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Namespace:    namespace.Name,
			},
		}

		err := c.Create(ctx, &dockyardsCluster)
		if err != nil {
			t.Fatal(err)
		}

		cluster := clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dockyardsCluster.Name,
				Namespace: dockyardsCluster.Namespace,
			},
		}

		err = c.Create(ctx, &cluster)
		if err != nil {
			t.Fatal(err)
		}

		patch := client.MergeFrom(cluster.DeepCopy())

		cluster.Status.V1Beta2 = &clusterv1.ClusterV1Beta2Status{
			Conditions: []metav1.Condition{
				{
					Type:    clusterv1.AvailableV1Beta2Condition,
					Status:  metav1.ConditionTrue,
					Reason:  clusterv1.AvailableV1Beta2Reason,
					Message: "",
				},
			},
		}

		err = c.Status().Patch(ctx, &cluster, patch)
		if err != nil {
			t.Fatal(err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      dockyardsCluster.Name,
				Namespace: dockyardsCluster.Namespace,
			},
		}

		_, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		var actual dockyardsv1.Cluster
		err = c.Get(ctx, client.ObjectKeyFromObject(&dockyardsCluster), &actual)
		if err != nil {
			t.Fatal(err)
		}

		expected := dockyardsv1.Cluster{
			ObjectMeta: actual.ObjectMeta,
			Status: dockyardsv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   dockyardsv1.ReadyCondition,
						Status: metav1.ConditionTrue,
						Reason: dockyardsv1.ReadyReason,
					},
					{
						Type:   ClusterAvailableCondition,
						Status: metav1.ConditionTrue,
						Reason: clusterv1.AvailableV1Beta2Reason,
					},
					{
						Type:   ClusterComponentsReadyCondition,
						Status: metav1.ConditionTrue,
						Reason: dockyardsv1.ReadyReason,
					},
				},
				Version: "3.2.1",
			},
		}

		if !cmp.Equal(actual, expected, opts) {
			t.Errorf("diff: %s", cmp.Diff(expected, actual, opts))
		}
	})

	t.Run("test deprecated conditions", func(t *testing.T) {
		dockyardsCluster := dockyardsv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Namespace:    namespace.Name,
			},
		}

		err := c.Create(ctx, &dockyardsCluster)
		if err != nil {
			t.Fatal(err)
		}

		cluster := clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dockyardsCluster.Name,
				Namespace: dockyardsCluster.Namespace,
			},
		}

		err = c.Create(ctx, &cluster)
		if err != nil {
			t.Fatal(err)
		}

		patch := client.MergeFrom(cluster.DeepCopy())

		cluster.Status.Conditions = clusterv1.Conditions{
			{
				Type:   clusterv1.ReadyCondition,
				Status: corev1.ConditionTrue,
			},
		}

		err = c.Status().Patch(ctx, &cluster, patch)
		if err != nil {
			t.Fatal(err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      dockyardsCluster.Name,
				Namespace: dockyardsCluster.Namespace,
			},
		}

		_, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		var actual dockyardsv1.Cluster
		err = c.Get(ctx, client.ObjectKeyFromObject(&dockyardsCluster), &actual)
		if err != nil {
			t.Fatal(err)
		}

		expected := dockyardsv1.Cluster{
			ObjectMeta: actual.ObjectMeta,
			Status: dockyardsv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   dockyardsv1.ReadyCondition,
						Status: metav1.ConditionFalse,
						Reason: WaitingForClusterControlPlaneReadyConditionReason,
					},
					{
						Type:   ClusterComponentsReadyCondition,
						Status: metav1.ConditionTrue,
						Reason: dockyardsv1.ReadyCondition,
					},
					{
						Type:   ClusterControlPlaneReadyCondition,
						Status: metav1.ConditionFalse,
						Reason: WaitingForClusterControlPlaneReadyConditionReason,
					},
					{
						Type:   ClusterReadyCondition,
						Status: metav1.ConditionTrue,
						Reason: dockyardsv1.ReadyReason,
					},
				},
				Version: "3.2.1",
			},
		}

		if !cmp.Equal(actual, expected, opts) {
			t.Errorf("diff: %s", cmp.Diff(expected, actual, opts))
		}
	})

	t.Run("test waiting conditions", func(t *testing.T) {
		dockyardsCluster := dockyardsv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Namespace:    namespace.Name,
			},
		}

		err := c.Create(ctx, &dockyardsCluster)
		if err != nil {
			t.Fatal(err)
		}

		patch := client.MergeFrom(dockyardsCluster.DeepCopy())

		dockyardsCluster.Status.Conditions = []metav1.Condition{
			{
				Type:   ClusterReadyCondition,
				Status: metav1.ConditionFalse,
				Reason: WaitingForClusterReadyConditionReason,
			},
			{
				Type:   ClusterControlPlaneReadyCondition,
				Status: metav1.ConditionFalse,
				Reason: WaitingForClusterControlPlaneReadyConditionReason,
			},
		}

		err = c.Status().Patch(ctx, &dockyardsCluster, patch)
		if err != nil {
			t.Fatal(err)
		}

		cluster := clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dockyardsCluster.Name,
				Namespace: dockyardsCluster.Namespace,
			},
		}

		err = c.Create(ctx, &cluster)
		if err != nil {
			t.Fatal(err)
		}

		patch = client.MergeFrom(cluster.DeepCopy())

		cluster.Status.V1Beta2 = &clusterv1.ClusterV1Beta2Status{
			Conditions: []metav1.Condition{
				{
					Type:    clusterv1.AvailableV1Beta2Condition,
					Status:  metav1.ConditionFalse,
					Reason:  clusterv1.ClusterAvailableInternalErrorV1Beta2Reason,
					Message: "",
				},
			},
		}

		err = c.Status().Patch(ctx, &cluster, patch)
		if err != nil {
			t.Fatal(err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      dockyardsCluster.Name,
				Namespace: dockyardsCluster.Namespace,
			},
		}

		_, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		var actual dockyardsv1.Cluster
		err = c.Get(ctx, client.ObjectKeyFromObject(&dockyardsCluster), &actual)
		if err != nil {
			t.Fatal(err)
		}

		expected := dockyardsv1.Cluster{
			ObjectMeta: actual.ObjectMeta,
			Status: dockyardsv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   dockyardsv1.ReadyCondition,
						Status: metav1.ConditionFalse,
						Reason: clusterv1.InternalErrorV1Beta2Reason,
					},
					{
						Type:   ClusterAvailableCondition,
						Status: metav1.ConditionFalse,
						Reason: clusterv1.InternalErrorV1Beta2Reason,
					},
					{
						Type:   ClusterComponentsReadyCondition,
						Status: metav1.ConditionTrue,
						Reason: dockyardsv1.ReadyReason,
					},
				},
				Version: "3.2.1",
			},
		}

		if !cmp.Equal(actual, expected, opts) {
			t.Errorf("diff: %s", cmp.Diff(expected, actual, opts))
		}
	})
}
