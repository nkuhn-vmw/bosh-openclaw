package broker

import (
	"encoding/json"
	"net/http"
)

type CatalogResponse struct {
	Services []Service `json:"services"`
}

type Service struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	Description          string                 `json:"description"`
	Bindable             bool                   `json:"bindable"`
	PlanUpdatable        bool                   `json:"plan_updateable"`
	InstancesRetrievable bool                   `json:"instances_retrievable"`
	BindingsRetrievable  bool                   `json:"bindings_retrievable"`
	Plans                []ServicePlan          `json:"plans"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
	Tags                 []string               `json:"tags"`
}

type ServicePlan struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Free        bool                   `json:"free"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func (b *Broker) Catalog(w http.ResponseWriter, r *http.Request) {
	plans := b.buildServicePlans()

	catalog := CatalogResponse{
		Services: []Service{
			{
				ID:                   "openclaw-service",
				Name:                 "openclaw",
				Description:          "Dedicated OpenClaw AI agent on an isolated VM with WebChat UI",
				Bindable:             true,
				PlanUpdatable:        true,
				InstancesRetrievable: true,
				BindingsRetrievable:  true,
				Plans:                plans,
				Tags:                 []string{"ai", "agent", "openclaw", "llm"},
				Metadata: map[string]interface{}{
					"displayName":         "OpenClaw AI Agent",
					"imageUrl":            IconDataURI(),
					"longDescription":     "Deploy a dedicated OpenClaw AI agent with persistent memory, shell access, browser automation, and 50+ integrations. Each instance runs on its own isolated VM with a dedicated WebChat UI.",
					"providerDisplayName": "OpenClaw Platform",
					"documentationUrl":    "https://github.com/openclaw/openclaw",
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(catalog)
}

func (b *Broker) buildServicePlans() []ServicePlan {
	configPlans := b.config.Plans
	if len(configPlans) == 0 {
		configPlans = defaultPlans()
	}

	plans := make([]ServicePlan, 0, len(configPlans))
	for _, p := range configPlans {
		sp := ServicePlan{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Free:        false,
			Metadata:    p.Metadata,
		}
		plans = append(plans, sp)
	}
	return plans
}
