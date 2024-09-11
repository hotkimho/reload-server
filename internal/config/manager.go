package config

import (
	"crypto/tls"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type ManagerConfig struct {
	ProbeAddr            string         `json:"probeAddr"`
	EnableLeaderElection bool           `json:"enableLeaderElection"`
	LeaderElectionID     string         `json:"leaderElectionID"`
	MetricsAddr          string         `json:"metricsAddr"`
	SecureMetrics        bool           `json:"secureMetrics"`
	EnableHTTP2          bool           `json:"enableHTTP2"`
	Metrics              server.Options `json:"-"`
	WebhookServer        webhook.Server `json:"-"`
}

// Default 값으로 ManagerConfig 생성
func newManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		MetricsAddr:          "0",
		ProbeAddr:            ":8081",
		EnableLeaderElection: false,
		SecureMetrics:        true,
		EnableHTTP2:          false,
		LeaderElectionID:     "dd36baba.accordions.edu",
	}
}

func (c *ManagerConfig) SetTLS() {
	var tlsOpts []func(*tls.Config)

	disableHTTP2 := func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}

	if !c.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	c.Metrics = server.Options{
		BindAddress:   c.MetricsAddr,
		SecureServing: c.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	c.WebhookServer = webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	if c.SecureMetrics {
		c.Metrics.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
}

func (c *ManagerConfig) ConvertCtrlOption(scheme *runtime.Scheme) ctrl.Options {
	return ctrl.Options{
		Scheme:                 scheme,
		Metrics:                c.Metrics,
		WebhookServer:          c.WebhookServer,
		HealthProbeBindAddress: c.ProbeAddr,
		LeaderElection:         c.EnableLeaderElection,
		LeaderElectionID:       c.LeaderElectionID,
	}
}
