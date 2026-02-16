package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/broker"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

func main() {
	configPath := flag.String("config", "/var/vcap/jobs/openclaw-broker/config/config.json", "Path to broker config")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate auth credentials are non-empty to prevent bypass
	if cfg.Auth.Username == "" || cfg.Auth.Password == "" {
		log.Fatalf("Broker auth credentials must not be empty (username=%q)", cfg.Auth.Username)
	}

	director := bosh.NewClient(cfg.BOSH.DirectorURL, cfg.BOSH.ClientID, cfg.BOSH.ClientSecret, cfg.BOSH.CACert, cfg.BOSH.UaaURL)

	// Use on_demand plans if available, fall back to top-level plans
	plans := cfg.OnDemand.Plans
	if len(plans) == 0 {
		plans = cfg.Plans
	}

	brokerCfg := broker.BrokerConfig{
		MinOpenClawVersion:     cfg.Security.MinOpenClawVersion,
		ControlUIEnabled:       cfg.Security.ControlUIEnabled,
		SandboxMode:            cfg.Security.SandboxMode,
		OpenClawVersion:        cfg.AgentDefaults.OpenClawVersion,
		Plans:                  plans,
		AppsDomain:             cfg.CF.AppsDomain,
		Network:                cfg.OnDemand.Network,
		AZs:                    cfg.OnDemand.AZs,
		StemcellOS:             cfg.OnDemand.StemcellOS,
		StemcellVersion:        cfg.OnDemand.StemcellVersion,
		CFDeploymentName:       cfg.CF.DeploymentName,
		OpenClawReleaseVersion: cfg.OnDemand.OpenClawReleaseVersion,
		BPMReleaseVersion:      cfg.OnDemand.BPMReleaseVersion,
		RoutingReleaseVersion:  cfg.OnDemand.RoutingReleaseVersion,
		SSOEnabled:             cfg.Security.SSOEnabled,
		SSOClientID:            cfg.Security.SSOClientID,
		SSOClientSecret:        cfg.Security.SSOClientSecret,
		SSOCookieSecret:        cfg.Security.SSOCookieSecret,
		SSOOIDCIssuerURL:       cfg.Security.SSOOIDCIssuerURL,
		MaxInstances:           cfg.Limits.MaxInstances,
		MaxInstancesPerOrg:     cfg.Limits.MaxInstancesPerOrg,
		LLMProvider:            cfg.GenAI.Provider,
		LLMEndpoint:            cfg.GenAI.Endpoint,
		LLMAPIKey:              cfg.GenAI.APIKey,
		LLMModel:               cfg.GenAI.Model,
		LLMAPIEndpoint:         cfg.GenAI.APIEndpoint,
		BlockedCommands:        cfg.Security.BlockedCommands,
		NATSTLSEnabled:         cfg.NATS.TLS.Enabled,
		NATSTLSClientCert:      cfg.NATS.TLS.ClientCert,
		NATSTLSClientKey:       cfg.NATS.TLS.ClientKey,
		NATSTLSCACert:          cfg.NATS.TLS.CACert,
	}
	b := broker.New(brokerCfg, director)

	log.Printf("Broker config: AZs=%v Network=%q StemcellOS=%q CFDeployment=%q SSOEnabled=%v",
		brokerCfg.AZs, brokerCfg.Network, brokerCfg.StemcellOS, brokerCfg.CFDeploymentName, brokerCfg.SSOEnabled)
	log.Printf("Broker config: Plans=%d MaxInstances=%d MaxPerOrg=%d MinVersion=%q",
		len(brokerCfg.Plans), brokerCfg.MaxInstances, brokerCfg.MaxInstancesPerOrg, brokerCfg.MinOpenClawVersion)

	r := mux.NewRouter()
	r.Use(basicAuthMiddleware(cfg.Auth.Username, cfg.Auth.Password))

	r.HandleFunc("/v2/catalog", b.Catalog).Methods("GET")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Provision).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Deprovision).Methods("DELETE")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Update).Methods("PATCH")
	r.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Bind).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Unbind).Methods("DELETE")
	r.HandleFunc("/v2/service_instances/{instance_id}/last_operation", b.LastOperation).Methods("GET")

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("OpenClaw broker starting on port %d", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down broker...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}
	log.Println("Broker stopped")
}

type Config struct {
	Port int `json:"port"`
	Auth struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"auth"`
	BOSH struct {
		DirectorURL  string `json:"director_url"`
		UaaURL       string `json:"uaa_url"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		CACert       string `json:"ca_cert"`
	} `json:"bosh"`
	AgentDefaults struct {
		OpenClawVersion string `json:"openclaw_version"`
		Stemcell        string `json:"stemcell"`
		Network         string `json:"network"`
		AZ              string `json:"az"`
	} `json:"agent_defaults"`
	Security struct {
		MinOpenClawVersion string `json:"min_openclaw_version"`
		ControlUIEnabled   bool   `json:"control_ui_enabled"`
		SandboxMode        string `json:"sandbox_mode"`
		BlockedCommands    string `json:"blocked_commands"`
		SSOEnabled         bool   `json:"sso_enabled"`
		SSOClientID        string `json:"sso_client_id"`
		SSOClientSecret    string `json:"sso_client_secret"`
		SSOCookieSecret    string `json:"sso_cookie_secret"`
		SSOOIDCIssuerURL   string `json:"sso_oidc_issuer_url"`
	} `json:"security"`
	GenAI struct {
		Provider     string `json:"provider"`
		Endpoint     string `json:"endpoint"`
		APIKey       string `json:"api_key"`
		Model        string `json:"model"`
		APIEndpoint  string `json:"api_endpoint"`
		OfferingName string `json:"offering_name"`
		PlanName     string `json:"plan_name"`
	} `json:"genai"`
	NATS struct {
		TLS struct {
			Enabled    bool   `json:"enabled"`
			ClientCert string `json:"client_cert"`
			ClientKey  string `json:"client_key"`
			CACert     string `json:"ca_cert"`
		} `json:"tls"`
	} `json:"nats"`
	OnDemand struct {
		ServiceName            string        `json:"service_name"`
		Plans                  []broker.Plan `json:"plans"`
		StemcellOS             string        `json:"stemcell_os"`
		StemcellVersion        string        `json:"stemcell_version"`
		Network                string        `json:"network"`
		AZs                    []string      `json:"azs"`
		OpenClawReleaseVersion string        `json:"openclaw_release_version"`
		BPMReleaseVersion      string        `json:"bpm_release_version"`
		RoutingReleaseVersion  string        `json:"routing_release_version"`
	} `json:"on_demand"`
	CF struct {
		SystemDomain   string `json:"system_domain"`
		AppsDomain     string `json:"apps_domain"`
		DeploymentName string `json:"deployment_name"`
	} `json:"cf"`
	Plans  []broker.Plan `json:"plans"`
	Limits struct {
		MaxInstances       int `json:"max_instances"`
		MaxInstancesPerOrg int `json:"max_instances_per_org"`
	} `json:"limits"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	return &cfg, nil
}

func basicAuthMiddleware(username, password string) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="OpenClaw Broker"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
