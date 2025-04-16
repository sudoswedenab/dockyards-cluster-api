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

func TestDockyardsNodeController_Reconcile(t *testing.T) {
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

	r := DockyardsNodeReconciler{
		Client: c,
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime"),
	}

	t.Run("test v1beta2 conditions", func(t *testing.T) {
		owner := clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-v1beta2",
				Namespace: namespace.Name,
			},
		}

		err := c.Create(ctx, &owner)
		if err != nil {
			t.Fatal(err)
		}

		patch := client.MergeFrom(owner.DeepCopy())

		owner.Status.V1Beta2 = &clusterv1.MachineV1Beta2Status{
			Conditions: []metav1.Condition{
				{
					Message: "v1beta2 testing",
					Reason:  clusterv1.MachineNotReadyV1Beta2Reason,
					Status:  metav1.ConditionFalse,
					Type:    clusterv1.MachineReadyV1Beta2Condition,
				},
			},
		}

		owner.Status.Conditions = clusterv1.Conditions{
			{
				Message: "v1beta1 testing",
				Type:    clusterv1.ReadyCondition,
				Reason:  "test",
				Status:  corev1.ConditionTrue,
			},
		}

		err = c.Status().Patch(ctx, &owner, patch)
		if err != nil {
			t.Fatal(err)
		}

		node := dockyardsv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{
					DockyardsNodeFinalizer,
				},
				Name:      "test-v1beta2",
				Namespace: namespace.Name,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Machine",
						Name:       owner.Name,
						UID:        owner.UID,
					},
				},
			},
		}

		err = c.Create(ctx, &node)
		if err != nil {
			t.Fatal(err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      node.Name,
				Namespace: node.Namespace,
			},
		}

		_, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		var actual dockyardsv1.Node
		err = c.Get(ctx, client.ObjectKeyFromObject(&node), &actual)
		if err != nil {
			t.Fatal(err)
		}

		expected := dockyardsv1.Node{
			ObjectMeta: actual.ObjectMeta,
			Status: dockyardsv1.NodeStatus{
				Conditions: []metav1.Condition{
					{
						Type:    dockyardsv1.ReadyCondition,
						Status:  metav1.ConditionFalse,
						Reason:  clusterv1.MachineNotReadyV1Beta2Reason,
						Message: "v1beta2 testing",
					},
					{
						Type:    MachineReadyCondition,
						Status:  metav1.ConditionFalse,
						Reason:  clusterv1.MachineNotReadyV1Beta2Reason,
						Message: "v1beta2 testing",
					},
				},
			},
		}

		if !cmp.Equal(actual, expected, opts) {
			t.Errorf("diff: %s", cmp.Diff(expected, actual, opts))
		}
	})

	t.Run("test deprecated conditions", func(t *testing.T) {
		owner := clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deprecated",
				Namespace: namespace.Name,
			},
		}

		err := c.Create(ctx, &owner)
		if err != nil {
			t.Fatal(err)
		}

		patch := client.MergeFrom(owner.DeepCopy())

		owner.Status.Conditions = clusterv1.Conditions{
			{
				Type:    clusterv1.ReadyCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "TestReady",
				Message: "testing ready",
			},
			{
				Type:    clusterv1.MachineNodeHealthyCondition,
				Status:  corev1.ConditionUnknown,
				Reason:  "TestNodeHealthy",
				Message: "testing node healthy",
			},
		}

		err = c.Status().Patch(ctx, &owner, patch)
		if err != nil {
			t.Fatal(err)
		}

		node := dockyardsv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{
					DockyardsNodeFinalizer,
				},
				Name:      "test-deprecated",
				Namespace: namespace.Name,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Machine",
						Name:       owner.Name,
						UID:        owner.UID,
					},
				},
			},
		}

		err = c.Create(ctx, &node)
		if err != nil {
			t.Fatal(err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      node.Name,
				Namespace: node.Namespace,
			},
		}

		_, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		var actual dockyardsv1.Node
		err = c.Get(ctx, client.ObjectKeyFromObject(&node), &actual)
		if err != nil {
			t.Fatal(err)
		}

		expected := dockyardsv1.Node{
			ObjectMeta: actual.ObjectMeta,
			Status: dockyardsv1.NodeStatus{
				Conditions: []metav1.Condition{
					{
						Type:    dockyardsv1.ReadyCondition,
						Status:  metav1.ConditionTrue,
						Message: "testing ready",
						Reason:  "TestReady",
					},
					{
						Type:    MachineReadyCondition,
						Status:  metav1.ConditionTrue,
						Message: "testing ready",
						Reason:  "TestReady",
					},
					{
						Type:    string(clusterv1.MachineNodeHealthyCondition),
						Status:  metav1.ConditionUnknown,
						Message: "testing node healthy",
						Reason:  "TestNodeHealthy",
					},
				},
			},
		}

		if !cmp.Equal(actual, expected, opts) {
			t.Errorf("diff: %s", cmp.Diff(expected, actual, opts))
		}
	})

	t.Run("test waiting conditions", func(t *testing.T) {
		owner := clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-waiting",
				Namespace: namespace.Name,
			},
		}

		err := c.Create(ctx, &owner)
		if err != nil {
			t.Fatal(err)
		}

		patch := client.MergeFrom(owner.DeepCopy())

		owner.Status.V1Beta2 = &clusterv1.MachineV1Beta2Status{
			Conditions: []metav1.Condition{
				{
					Message: "v1beta2 testing",
					Reason:  clusterv1.MachineReadyV1Beta2Reason,
					Status:  metav1.ConditionTrue,
					Type:    clusterv1.MachineReadyV1Beta2Condition,
				},
			},
		}

		owner.Status.Conditions = clusterv1.Conditions{
			{
				Message: "v1beta1 testing",
				Type:    clusterv1.ReadyCondition,
				Reason:  "test",
				Status:  corev1.ConditionTrue,
			},
		}

		err = c.Status().Patch(ctx, &owner, patch)
		if err != nil {
			t.Fatal(err)
		}

		node := dockyardsv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{
					DockyardsNodeFinalizer,
				},
				Name:      "test-waiting",
				Namespace: namespace.Name,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Machine",
						Name:       owner.Name,
						UID:        owner.UID,
					},
				},
			},
		}

		err = c.Create(ctx, &node)
		if err != nil {
			t.Fatal(err)
		}

		patch = client.MergeFrom(node.DeepCopy())

		node.Status.Conditions = []metav1.Condition{
			{
				Type:   dockyardsv1.ReadyCondition,
				Status: metav1.ConditionFalse,
				Reason: WaitingForMachineReadyConditionReason,
			},
			{
				Type:   MachineReadyCondition,
				Status: metav1.ConditionFalse,
				Reason: WaitingForMachineReadyConditionReason,
			},
			{
				Type:   string(clusterv1.MachineNodeHealthyCondition),
				Status: metav1.ConditionFalse,
				Reason: WaitingForNodeHealthyConditionReason,
			},
		}

		err = c.Status().Patch(ctx, &node, patch)
		if err != nil {
			t.Fatal(err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      node.Name,
				Namespace: node.Namespace,
			},
		}

		_, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		var actual dockyardsv1.Node
		err = c.Get(ctx, client.ObjectKeyFromObject(&node), &actual)
		if err != nil {
			t.Fatal(err)
		}

		expected := dockyardsv1.Node{
			ObjectMeta: actual.ObjectMeta,
			Status: dockyardsv1.NodeStatus{
				Conditions: []metav1.Condition{
					{
						Type:    dockyardsv1.ReadyCondition,
						Status:  metav1.ConditionTrue,
						Reason:  clusterv1.ReadyV1Beta2Reason,
						Message: "v1beta2 testing",
					},
					{
						Type:    MachineReadyCondition,
						Status:  metav1.ConditionTrue,
						Reason:  clusterv1.ReadyV1Beta2Reason,
						Message: "v1beta2 testing",
					},
				},
			},
		}

		if !cmp.Equal(actual, expected, opts) {
			t.Errorf("diff: %s", cmp.Diff(expected, actual, opts))
		}
	})
}
