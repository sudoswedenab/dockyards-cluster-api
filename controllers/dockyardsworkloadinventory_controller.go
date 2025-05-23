package controllers

import (
	"context"
	"errors"
	"net/url"

	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3"
	"bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3/index"
	"github.com/fluxcd/pkg/runtime/patch"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// +kubebuilder:rbac:groups=dockyards.io,resources=workloadinventories,verbs=get;list;patch;watch

type DockyardsWorkloadInventoryReconciler struct {
	client.Client

	ClusterCache clustercache.ClusterCache
	controller   controller.Controller
}

func (r *DockyardsWorkloadInventoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	logger := ctrl.LoggerFrom(ctx)

	var workloadInventory dockyardsv1.WorkloadInventory
	err := r.Get(ctx, req.NamespacedName, &workloadInventory)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !workloadInventory.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	dockyardsCluster, hasLabel := workloadInventory.Labels[dockyardsv1.LabelClusterName]
	if !hasLabel {
		logger.Info("ignore without cluster name label")

		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(&workloadInventory, r)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		err := patchHelper.Patch(ctx, &workloadInventory)
		if err != nil {
			result = ctrl.Result{}
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	clusterName, hasLabel := workloadInventory.Labels[clusterv1.ClusterNameLabel]
	if !hasLabel {
		workloadInventory.Labels[clusterv1.ClusterNameLabel] = dockyardsCluster

		return ctrl.Result{}, nil
	}

	objectKey := client.ObjectKey{
		Name:      clusterName,
		Namespace: workloadInventory.Namespace,
	}

	err = r.ClusterCache.Watch(ctx, objectKey, clustercache.NewWatcher(clustercache.WatcherOptions{
		Name:         "workloadInventory-watchIngresses",
		Watcher:      r.controller,
		Kind:         &networkingv1.Ingress{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.ingressToWorkloadInventory),
	}))
	if err != nil {
		if errors.Is(err, clustercache.ErrClusterNotConnected) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	remoteReader, err := r.ClusterCache.GetReader(ctx, objectKey)
	if err != nil {
		if errors.Is(err, clustercache.ErrClusterNotConnected) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	matchingLabels, err := metav1.LabelSelectorAsMap(&workloadInventory.Spec.Selector)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("listing remote ingresses", "matchingLabels", matchingLabels)

	var ingressList networkingv1.IngressList
	err = remoteReader.List(ctx, &ingressList, client.MatchingLabels(matchingLabels))
	if err != nil {
		logger.Error(err, "error listing remote ingresses")

		return ctrl.Result{}, nil
	}

	urls := []string{}
	for _, ingress := range ingressList.Items {
		if len(ingress.Status.LoadBalancer.Ingress) == 0 {
			continue
		}

		if len(ingress.Spec.TLS) > 0 {
			for _, tls := range ingress.Spec.TLS {
				for _, host := range tls.Hosts {
					u := url.URL{
						Scheme: "https",
						Host:   host,
					}

					urls = append(urls, u.String())
				}
			}

			continue
		}

		for _, rule := range ingress.Spec.Rules {
			logger.Info("ingress", "ingressName", ingress.Name, "ingressNamespace", ingress.Namespace, "host", rule.Host)
			u := url.URL{
				Scheme: "http",
				Host:   rule.Host,
			}

			urls = append(urls, u.String())
		}
	}

	workloadInventory.Spec.URLs = urls

	logger.Info("inventory", "urls", urls)

	return ctrl.Result{}, nil
}

func (r *DockyardsWorkloadInventoryReconciler) ingressToWorkloadInventory(ctx context.Context, obj client.Object) []ctrl.Request {
	logger := ctrl.LoggerFrom(ctx)

	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil
	}

	logger.Info("ingress", "name", ingress.Name, "namespace", ingress.Namespace)

	matchingFields := client.MatchingFields{
		index.SelectorField: index.MatchLabelsSummary(ingress.Labels),
	}

	var workloadInventoryList dockyardsv1.WorkloadInventoryList
	err := r.List(ctx, &workloadInventoryList, matchingFields)
	if err != nil {
		logger.Error(err, "error listing workload inventory")

		return nil
	}

	requests := []ctrl.Request{}
	for _, workloadInventory := range workloadInventoryList.Items {
		requests = append(requests, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      workloadInventory.Name,
				Namespace: workloadInventory.Namespace,
			},
		})
	}

	return requests
}

func (r *DockyardsWorkloadInventoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	scheme := mgr.GetScheme()

	_ = dockyardsv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	clusterToWorkloadInventories, err := util.ClusterToTypedObjectsMapper(mgr.GetClient(), &dockyardsv1.WorkloadInventoryList{}, mgr.GetScheme())
	if err != nil {
		return err
	}

	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&dockyardsv1.WorkloadInventory{}).
		WatchesRawSource(r.ClusterCache.GetClusterSource("workloadinventories", clusterToWorkloadInventories)).
		Build(r)
	if err != nil {
		return err
	}

	r.controller = controller

	return nil
}
