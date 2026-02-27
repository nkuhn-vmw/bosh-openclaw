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
	"path/filepath"
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

	// Load GenAI credentials from marketplace service key (if tanzu_genai provider)
	if cfg.GenAI.Provider == "tanzu_genai" {
		configDir := filepath.Dir(*configPath)
		endpoint, apiKey, model, err := loadGenAICredentials(configDir)
		if err != nil {
			log.Fatalf("Failed to load GenAI marketplace credentials: %v", err)
		}
		cfg.GenAI.Endpoint = endpoint
		cfg.GenAI.APIKey = apiKey
		if model != "" && cfg.GenAI.Model == "" {
			cfg.GenAI.Model = model
		}
		log.Printf("GenAI: loaded marketplace credentials, endpoint=%s model=%s", endpoint, cfg.GenAI.Model)
	}

	director := bosh.NewClient(cfg.BOSH.DirectorURL, cfg.BOSH.ClientID, cfg.BOSH.ClientSecret, cfg.BOSH.CACert, cfg.BOSH.UaaURL)

	// Use on_demand plans if available, fall back to top-level plans
	plans := cfg.OnDemand.Plans
	if len(plans) == 0 {
		plans = cfg.Plans
	}

	brokerCfg := broker.BrokerConfig{
		MinOpenClawVersion:     cfg.Security.MinOpenClawVersion,
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
		SSOEnabled:              cfg.Security.SSOEnabled,
		SSOOIDCIssuerURL:        cfg.Security.SSOOIDCIssuerURL,
		SSOAllowedEmailDomains:  cfg.Security.SSOAllowedEmailDomains,
		SSOSessionTimeoutHours:  cfg.Security.SSOSessionTimeoutHours,
		CFUaaURL:                cfg.CFUAA.URL,
		CFUaaAdminClientID:      cfg.CFUAA.AdminClientID,
		CFUaaAdminClientSecret:  cfg.CFUAA.AdminClientSecret,
		MaxInstances:           cfg.Limits.MaxInstances,
		MaxInstancesPerOrg:     cfg.Limits.MaxInstancesPerOrg,
		LLMProvider:            cfg.GenAI.Provider,
		LLMEndpoint:            cfg.GenAI.Endpoint,
		LLMAPIKey:              cfg.GenAI.APIKey,
		LLMModel:               cfg.GenAI.Model,
		LLMPreferredModel:      cfg.GenAI.PreferredModel,
		LLMAPIEndpoint:         cfg.GenAI.APIEndpoint,
		GenAIOfferingName:      cfg.GenAI.OfferingName,
		GenAIPlanName:          cfg.GenAI.PlanName,
		BlockedCommands:        cfg.Security.BlockedCommands,
		NATSTLSEnabled:         cfg.NATS.TLS.Enabled,
		NATSTLSClientCert:      cfg.NATS.TLS.ClientCert,
		NATSTLSClientKey:       cfg.NATS.TLS.ClientKey,
		NATSTLSCACert:          cfg.NATS.TLS.CACert,
		StateDir:               "/var/vcap/store/openclaw-broker",
	}
	b := broker.New(brokerCfg, director)

	log.Printf("Broker config: AZs=%v Network=%q StemcellOS=%q CFDeployment=%q SSOEnabled=%v",
		brokerCfg.AZs, brokerCfg.Network, brokerCfg.StemcellOS, brokerCfg.CFDeploymentName, brokerCfg.SSOEnabled)
	log.Printf("Broker config: Plans=%d MaxInstances=%d MaxPerOrg=%d MinVersion=%q",
		len(brokerCfg.Plans), brokerCfg.MaxInstances, brokerCfg.MaxInstancesPerOrg, brokerCfg.MinOpenClawVersion)
	uaaConfigured := brokerCfg.CFUaaURL != "" && brokerCfg.CFUaaAdminClientSecret != ""
	log.Printf("Broker SSO: enabled=%v uaa_configured=%v issuer=%q uaa_url=%q",
		brokerCfg.SSOEnabled, uaaConfigured, brokerCfg.SSOOIDCIssuerURL, brokerCfg.CFUaaURL)
	if brokerCfg.SSOEnabled && !uaaConfigured {
		log.Printf("WARNING: SSO is enabled but CF UAA admin credentials are not configured — SSO will be disabled for all instances")
	}

	r := mux.NewRouter()
	r.Use(basicAuthMiddleware(cfg.Auth.Username, cfg.Auth.Password))

	r.HandleFunc("/v2/catalog", b.Catalog).Methods("GET")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Provision).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Deprovision).Methods("DELETE")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Update).Methods("PATCH")
	r.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Bind).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Unbind).Methods("DELETE")
	r.HandleFunc("/v2/service_instances/{instance_id}/last_operation", b.LastOperation).Methods("GET")

	r.HandleFunc("/admin/instances", b.AdminListInstances).Methods("GET")
	r.HandleFunc("/admin/upgrade", b.AdminUpgrade).Methods("POST")
	r.HandleFunc("/admin/upgrade/status", b.AdminUpgradeStatus).Methods("GET")

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
		MinOpenClawVersion     string `json:"min_openclaw_version"`
		SandboxMode            string `json:"sandbox_mode"`
		BlockedCommands        string `json:"blocked_commands"`
		SSOEnabled             bool   `json:"sso_enabled"`
		SSOOIDCIssuerURL       string `json:"sso_oidc_issuer_url"`
		SSOAllowedEmailDomains string `json:"sso_allowed_email_domains"`
		SSOSessionTimeoutHours int    `json:"sso_session_timeout_hours"`
	} `json:"security"`
	CFUAA struct {
		URL               string `json:"url"`
		AdminClientID     string `json:"admin_client_id"`
		AdminClientSecret string `json:"admin_client_secret"`
	} `json:"cf_uaa"`
	GenAI struct {
		Provider     string `json:"provider"`
		Endpoint     string `json:"endpoint"`
		APIKey       string `json:"api_key"`
		Model          string `json:"model"`
		PreferredModel string `json:"preferred_model"`
		APIEndpoint    string `json:"api_endpoint"`
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
		SystemDomain      string `json:"system_domain"`
		AppsDomain        string `json:"apps_domain"`
		DeploymentName    string `json:"deployment_name"`
		APIURL            string `json:"api_url"`
		AdminUsername     string `json:"admin_username"`
		AdminPassword     string `json:"admin_password"`
		SkipSSLValidation bool   `json:"skip_ssl_validation"`
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

// GenAI service key credential structures
type GenAIEndpoint struct {
	APIBase   string `json:"api_base"`
	APIKey    string `json:"api_key"`
	ConfigURL string `json:"config_url"`
	Name      string `json:"name"`
}

type GenAIServiceKey struct {
	// Wrapped format: {"credentials": {...}}
	Credentials *GenAIServiceKeyCreds `json:"credentials"`
	// Unwrapped format: top-level fields
	GenAIServiceKeyCreds
}

type GenAIServiceKeyCreds struct {
	APIBase   string         `json:"api_base"`
	APIKey    string         `json:"api_key"`
	ModelName string         `json:"model_name"`
	Endpoint  *GenAIEndpoint `json:"endpoint"`
}

// discoverChatModel fetches the GenAI config URL and returns the first model with CHAT capability.
func discoverChatModel(configURL, apiKey string) (string, error) {
	req, err := http.NewRequest("GET", configURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching config URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("config URL returned status %d", resp.StatusCode)
	}

	var result struct {
		AdvertisedModels []struct {
			Name         string   `json:"name"`
			Capabilities []string `json:"capabilities"`
		} `json:"advertisedModels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing config response: %w", err)
	}

	for _, m := range result.AdvertisedModels {
		for _, cap := range m.Capabilities {
			if cap == "CHAT" {
				return m.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no model with CHAT capability found")
}

func loadGenAICredentials(configDir string) (endpoint, apiKey, model string, err error) {
	credsPath := filepath.Join(configDir, "genai-credentials.json")
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return "", "", "", fmt.Errorf("reading %s: %w", credsPath, err)
	}

	var key GenAIServiceKey
	if err := json.Unmarshal(data, &key); err != nil {
		return "", "", "", fmt.Errorf("parsing %s: %w", credsPath, err)
	}

	// Use wrapped credentials if present, otherwise use top-level
	creds := &key.GenAIServiceKeyCreds
	if key.Credentials != nil && (key.Credentials.APIBase != "" || key.Credentials.APIKey != "" || key.Credentials.Endpoint != nil) {
		creds = key.Credentials
	}

	endpoint = creds.APIBase
	apiKey = creds.APIKey
	model = creds.ModelName

	// Fall back to endpoint.api_base/api_key (multi-format binding)
	if endpoint == "" && creds.Endpoint != nil {
		endpoint = creds.Endpoint.APIBase
	}
	if apiKey == "" && creds.Endpoint != nil {
		apiKey = creds.Endpoint.APIKey
	}

	if endpoint == "" || apiKey == "" {
		return "", "", "", fmt.Errorf("genai-credentials.json missing api_base or api_key")
	}

	// Multi-model endpoints don't have model_name — discover the chat model from config URL
	if model == "" && creds.Endpoint != nil && creds.Endpoint.ConfigURL != "" {
		discovered, err := discoverChatModel(creds.Endpoint.ConfigURL, apiKey)
		if err != nil {
			log.Printf("WARNING: failed to discover chat model: %v", err)
		} else {
			model = discovered
			log.Printf("GenAI: discovered chat model: %s", model)
		}
	}

	return endpoint, apiKey, model, nil
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
