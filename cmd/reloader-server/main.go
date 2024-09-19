/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/hotkimho/reloader-server/project/internal/config"
	"github.com/hotkimho/reloader-server/project/pkg/controller"
	// +kubebuilder:scaffold:imports
)

const (
	KUBECONFIG = "kubeconfig"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

type flagConfig struct {
	configName      string
	configNamespace string
	configDataKey   string
	debugMode       bool
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	cfg := parseFlagConfig()

	opts := zap.Options{
		Development: cfg.debugMode,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("starting manager")
	if err := startManager(cfg, scheme); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
}

func parseFlagConfig() *flagConfig {
	flagCfg := &flagConfig{}

	flag.StringVar(&flagCfg.configName, "config-name", "reloader-server-config", "")
	flag.StringVar(&flagCfg.configNamespace, "config-namespace", "default", "")
	flag.StringVar(&flagCfg.configDataKey, "config-data-key", "config", "")
	flag.BoolVar(&flagCfg.debugMode, "debug", true, "debug mode")

	var kubeconfigPath string
	kubeConfig := flag.Lookup(KUBECONFIG)
	if kubeConfig != nil {
		kubeconfigPath = kubeConfig.Value.String()
	} else {
		flag.StringVar(&kubeconfigPath, KUBECONFIG, "$HOME/.kube/config", "")
	}
	os.Setenv("KUBECONFIG", kubeconfigPath)

	return flagCfg
}

func startManager(flag *flagConfig, scheme *runtime.Scheme) error {
	ctx := ctrl.SetupSignalHandler()

	kubeCfg, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "unable to get kubeconfig")
		return err
	}

	client, err := kubernetes.NewForConfig(kubeCfg)
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client")
		return err
	}

	cm, err := client.CoreV1().ConfigMaps(flag.configNamespace).Get(ctx, flag.configName, metav1.GetOptions{})
	if err != nil {
		setupLog.Error(err, "unable to get configmap")
		return err
	}

	cfg := config.NewConfig()
	if err := yaml.Unmarshal([]byte(cm.Data[flag.configDataKey]), &cfg); err != nil {
		setupLog.Error(err, "unable to unmarshal configmap data")
		return err
	}
	cfg.Manager.SetTLS()

	mgr, err := ctrl.NewManager(kubeCfg, cfg.Manager.ConvertCtrlOption(scheme))
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		return err
	}

	if err = controller.SetupReloaderController(mgr); err != nil {
		setupLog.Error(err, "unable to set up reloader controller")
		return err
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
