package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"bitbucket.org/sudosweden/dockyards-cluster-api/controllers"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
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

	options := manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: metricsBindAddress,
		},
	}

	m, err := ctrl.NewManager(cfg, options)
	if err != nil {
		slogr.Error(err, "error creating manager")

		os.Exit(1)
	}

	err = (&controllers.DockyardsClusterReconciler{
		Client: m.GetClient(),
	}).SetupWithManager(m)
	if err != nil {
		slogr.Error(err, "error creating dockyards cluster reconciler")

		os.Exit(1)
	}

	err = (&controllers.ClusterAPIMachineReconciler{
		Client: m.GetClient(),
	}).SetupWithManager(m)
	if err != nil {
		slogr.Error(err, "error creating cluster-api machine reconciler")

		os.Exit(1)
	}

	err = m.Start(ctx)
	if err != nil {
		slogr.Error(err, "error starting manager")

		os.Exit(1)
	}
}
