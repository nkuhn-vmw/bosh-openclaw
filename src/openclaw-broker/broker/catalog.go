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
	plans := []ServicePlan{
		{
			ID:          "openclaw-developer-plan",
			Name:        "developer",
			Description: "Dedicated OpenClaw agent for individual developers",
			Free:        false,
			Metadata: map[string]interface{}{
				"displayName": "Developer",
				"bullets": []string{
					"Dedicated VM with isolated WebChat UI",
					"2GB RAM, 10GB persistent disk",
					"Cloud LLM integration",
					"Per-instance SSO",
				},
			},
		},
		{
			ID:          "openclaw-developer-plus-plan",
			Name:        "developer-plus",
			Description: "Enhanced agent with browser automation",
			Free:        false,
			Metadata: map[string]interface{}{
				"displayName": "Developer Plus",
				"bullets": []string{
					"Dedicated VM with isolated WebChat UI",
					"4GB RAM, 20GB persistent disk",
					"Browser automation enabled",
					"All messaging channels",
				},
			},
		},
		{
			ID:          "openclaw-team-plan",
			Name:        "team",
			Description: "Shared agent for teams",
			Free:        false,
			Metadata: map[string]interface{}{
				"displayName": "Team",
				"bullets": []string{
					"Dedicated VM with isolated WebChat UI",
					"8GB RAM, 50GB persistent disk",
					"Multi-user with Slack/Teams",
					"Full browser automation",
				},
			},
		},
	}

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
