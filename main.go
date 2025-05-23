package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3/index"
	"bitbucket.org/sudosweden/dockyards-cluster-api/controllers"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	var metricsBindAddress string
	pflag.StringVar(&metricsBindAddress, "metrics-bind-address", "0", "metrics bind address")
	pflag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slogr := logr.FromSlogHandler(handler)

	ctrl.SetLogger(slogr)

	cfg, err := config.GetConfig()
	if err != nil {
		slogr.Error(err, "error getting config")

		os.Exit(1)
	}

	scheme := runtime.NewScheme()

	_ = clusterv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	options := manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: metricsBindAddress,
		},
		Scheme: scheme,
	}

	mgr, err := ctrl.NewManager(cfg, options)
	if err != nil {
		slogr.Error(err, "error creating manager")

		os.Exit(1)
	}

	secretClient, err := client.New(mgr.GetConfig(), client.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Cache: &client.CacheOptions{
			Reader: mgr.GetCache(),
		},
	})
	if err != nil {
		slogr.Error(err, "error creating secret client")

		os.Exit(1)
	}

	clusterCache, err := clustercache.SetupWithManager(ctx, mgr, clustercache.Options{
		SecretClient: secretClient,
		Client: clustercache.ClientOptions{
			UserAgent: "cluster-api.dockyards.io",
		},
	}, controller.Options{})
	if err != nil {
		slogr.Error(err, "error creating cluster cache")

		os.Exit(1)
	}

	err = (&controllers.DockyardsClusterReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr)
	if err != nil {
		slogr.Error(err, "error creating dockyards cluster reconciler")

		os.Exit(1)
	}

	err = (&controllers.MachineReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr)
	if err != nil {
		slogr.Error(err, "error creating machine reconciler")

		os.Exit(1)
	}

	err = (&controllers.DockyardsNodeReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr)
	if err != nil {
		slogr.Error(err, "error creating dockyards node reconciler")

		os.Exit(1)
	}

	err = (&controllers.DockyardsNodePoolReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr)
	if err != nil {
		slogr.Error(err, "error creating dockyards node pool reconciler")

		os.Exit(1)
	}

	err = (&controllers.DockyardsWorkloadInventoryReconciler{
		Client:       mgr.GetClient(),
		ClusterCache: clusterCache,
	}).SetupWithManager(mgr)
	if err != nil {
		slogr.Error(err, "error creating dockyards workload inventory reconciler")

		os.Exit(1)
	}

	err = index.BySelector(ctx, mgr)
	if err != nil {
		slogr.Error(err, "error adding by selector index")

		os.Exit(1)
	}

	err = mgr.Start(ctx)
	if err != nil {
		slogr.Error(err, "error starting manager")

		os.Exit(1)
	}
}
