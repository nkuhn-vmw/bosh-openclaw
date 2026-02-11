# `bosh-openclaw` — BOSH Release & Tanzu Ops Manager Tile

## OpenClaw Agent Platform for Cloud Foundry

**Author:** Nick Kuhn  
**Date:** February 11, 2026  
**Version:** 0.1.0-draft  
**Reference Architecture:** [`bosh-seaweedfs`](https://github.com/nkuhn-vmw/bosh-seaweedfs)

---

## 1. Overview

`bosh-openclaw` is a BOSH release and Tanzu Operations Manager tile that deploys [OpenClaw](https://github.com/openclaw/openclaw) as a managed, multi-tenant enterprise service on Cloud Foundry / Tanzu Platform. OpenClaw is the open-source AI agent framework (145K+ GitHub stars) built on Node.js that enables autonomous task execution via LLMs with persistent memory, shell access, browser automation, and 50+ messaging/tool integrations.

The tile provides:

- **Dedicated VMs per developer/team** — each OpenClaw agent runs on its own BOSH-managed VM with full OS-level isolation, persistent disk, and its own WebChat UI on a dedicated route
- **Self-service provisioning** — developers create agents via `cf create-service openclaw developer my-agent` and get a personal URL (e.g., `openclaw-alice.apps.example.com`)
- **Enterprise security controls** — centralized version enforcement, skills allowlisting, sandbox policies, network segmentation, and per-instance SSO
- **Operator management** — Ops Manager tile with configuration forms, errands, and an admin dashboard for fleet monitoring
- **LLM integration** — automatic Tanzu GenAI service configuration or direct Anthropic/OpenAI API key injection

---

## 2. Architecture

### 2.1 High-Level Overview

```
┌─────────────────────────────────────────────────────────────┐
│                  Tanzu Operations Manager                    │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              OpenClaw Tile Configuration              │    │
│  │  • GenAI provider settings                           │    │
│  │  • Skills allowlist                                  │    │
│  │  • VM sizing / plan definitions                      │    │
│  │  • Security policies                                 │    │
│  │  • Network assignments                               │    │
│  └─────────────────────────────────────────────────────┘    │
└──────────────────────────┬──────────────────────────────────┘
                           │ BOSH Deploy
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    BOSH Deployment                            │
│                                                              │
│  STATIC INFRASTRUCTURE (always running)                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  openclaw-    │  │  openclaw-   │  │  openclaw-   │      │
│  │  broker       │  │  registry    │  │  admin-dash  │      │
│  │  (1 VM)       │  │  (1 VM)      │  │  (1 VM)      │      │
│  │  + route-reg  │  │              │  │  + route-reg │      │
│  └──────┬───────┘  └──────────────┘  └──────────────┘      │
│         │                                      ▲             │
│         │ provisions on-demand                 │ fleet view  │
│         ▼                                      │             │
│  ON-DEMAND AGENT VMs (dedicated per developer)               │
│  ┌───────────────────┐  ┌───────────────────┐               │
│  │  openclaw-agent    │  │  openclaw-agent   │               │
│  │  instance: alice   │  │  instance: bob    │  ...          │
│  │  ┌───────────────┐│  │  ┌───────────────┐│               │
│  │  │ Gateway :18789││  │  │ Gateway :18789││               │
│  │  │ WebChat :8080 ││  │  │ WebChat :8080 ││               │
│  │  │ Node (system) ││  │  │ Node (system) ││               │
│  │  │ Skills runtime││  │  │ Skills runtime││               │
│  │  └───────────────┘│  │  └───────────────┘│               │
│  │  + route-registrar│  │  + route-registrar│               │
│  │  + sso-proxy      │  │  + sso-proxy      │               │
│  │  (optional)       │  │  (optional)       │               │
│  │                   │  │                   │               │
│  │  Route:           │  │  Route:           │               │
│  │  openclaw-alice   │  │  openclaw-bob     │               │
│  │  .apps.example.com│  │  .apps.example.com│               │
│  │  2GB / 10GB disk  │  │  4GB / 20GB disk  │               │
│  └───────────────────┘  └───────────────────┘               │
│                                                              │
│  CF Go Router ◄──── routes from each agent's route-registrar│
└─────────────────────────────────────────────────────────────┘
```

Each agent VM owns its own UI. Every provisioned instance runs its own WebChat interface on a dedicated route registered directly with the CF Go Router. Developers access their personal agent at a unique URL. The admin dashboard exists only for platform operators to monitor fleet health and enforce compliance.

### 2.2 Components

| Component | Job Name | Description | Instances |
|---|---|---|---|
| **OpenClaw Broker** | `openclaw-broker` | Open Service Broker API. Provisions/deprovisions dedicated agent VMs. Manages bindings with credentials. | 1 (static) |
| **OpenClaw Agent** | `openclaw-agent` | Dedicated OpenClaw gateway + node + WebChat UI on an isolated VM. Each instance gets its own route via colocated `route-registrar` and optional `sso-proxy`. Provisioned on-demand by the broker. | 0–N (dynamic) |
| **Skills Registry** | `openclaw-registry` | Curated skills repository. Hosts approved skills and enforces the allowlist. Syncs from upstream ClawHub with review gate. | 1 (static) |
| **Admin Dashboard** | `openclaw-admin-dashboard` | Operator-only web UI for fleet monitoring: instance health, usage metrics, version compliance, audit logs. | 1 (static) |
| **Route Registrar** | `route-registrar` | Colocated on each agent VM (and on broker/dashboard). Registers the instance's dedicated WebChat route with CF Go Router on deploy, deregisters on teardown. | colocated |
| **SSO Proxy** | `openclaw-sso-proxy` | Optional oauth2-proxy colocated on agent VMs. Authenticates users via CF `p-identity` before they reach the WebChat UI. Each instance gets its own SSO redirect URI. | colocated (opt) |
| **Register Broker** | `register-broker` | Errand: registers the service broker with CF. | errand |
| **Deregister Broker** | `deregister-broker` | Errand: deregisters the service broker from CF. | errand |
| **Smoke Tests** | `smoke-tests` | Errand: validates end-to-end provisioning, agent health, WebChat route accessibility, and LLM connectivity. | errand |
| **Upgrade Agents** | `upgrade-agents` | Errand: rolling upgrade of all provisioned agent VMs to the latest OpenClaw version. | errand |

### 2.3 Service Broker Plans

The broker exposes OpenClaw through the CF marketplace:

| Plan | VM Type | Memory | Disk | Use Case |
|---|---|---|---|---|
| `developer` | `small` | 2 GB | 10 GB | Individual developer; cloud LLM only |
| `developer-plus` | `medium` | 4 GB | 20 GB | Power user; browser automation + larger context |
| `team` | `large` | 8 GB | 50 GB | Shared team agent; multiple channels |

### 2.4 Provisioning Flow

```
Developer                    CF CLI / Apps Manager
    │                              │
    │  cf create-service           │
    │  openclaw developer          │
    │  my-agent                    │
    │─────────────────────────────>│
    │                              │
    │                    ┌─────────▼──────────┐
    │                    │  OpenClaw Broker    │
    │                    │                    │
    │                    │  1. Generate seed   │
    │                    │  2. Generate token  │
    │                    │  3. Derive route:   │
    │                    │     openclaw-alice  │
    │                    │     .apps.example   │
    │                    │  4. BOSH deploy     │
    │                    │     agent VM with:  │
    │                    │     - gateway       │
    │                    │     - webchat       │
    │                    │     - route-reg     │
    │                    │     - sso-proxy     │
    │                    │  5. Wait healthy    │
    │                    │  6. Verify route    │
    │                    │     registered      │
    │                    │  7. Return creds    │
    │                    └─────────┬──────────┘
    │                              │
    │                    ┌─────────▼──────────┐
    │                    │  Agent VM boots     │
    │                    │                    │
    │                    │  pre-start:        │
    │                    │  • Install OpenClaw │
    │                    │  • Sync skills     │
    │                    │  • Configure LLM   │
    │                    │                    │
    │                    │  route-registrar:  │
    │                    │  • Registers route │
    │                    │    with Go Router  │
    │                    │                    │
    │                    │  sso-proxy:        │
    │                    │  • Binds :8080     │
    │                    │  • Proxies to      │
    │                    │    WebChat :8081   │
    │                    │  (if SSO enabled)  │
    │                    └─────────┬──────────┘
    │                              │
    │  Developer opens browser:    │
    │  https://openclaw-alice      │
    │    .apps.example.com         │
    │         │                    │
    │         ▼                    │
    │  ┌──────────────────┐       │
    │  │ CF Go Router     │       │
    │  │ → SSO (optional) │       │
    │  │ → WebChat UI     │       │
    │  │ (dedicated to    │       │
    │  │  this developer) │       │
    │  └──────────────────┘       │
    │                              │
    │  cf bind-service my-app      │
    │  my-agent                    │
    │─────────────────────────────>│
    │                              │
    │  Credentials:                │
    │  {                           │
    │    webchat_url: https://     │
    │      openclaw-alice          │
    │      .apps.example.com      │
    │    gateway_url: wss://...    │
    │    gateway_token: ...        │
    │  }                           │
    │<─────────────────────────────│
```

---

## 3. BOSH Release Structure

```
bosh-openclaw/
├── .github/workflows/
│   └── ci.yml                      # Build + test pipeline
├── config/
│   ├── blobs.yml                   # Blob references (Node.js, OpenClaw tarball)
│   └── final.yml                   # Release name + blobstore config
├── jobs/
│   ├── openclaw-agent/
│   │   ├── spec                    # Job specification
│   │   ├── monit                   # Process monitoring
│   │   └── templates/
│   │       ├── ctl.erb             # Start/stop script
│   │       ├── openclaw.json.erb   # OpenClaw config template
│   │       ├── pre-start.erb       # Node.js setup, skills sync
│   │       ├── drain.erb           # Graceful shutdown
│   │       └── bpm.yml.erb         # BPM process config
│   ├── openclaw-broker/
│   │   ├── spec
│   │   ├── monit
│   │   └── templates/
│   │       ├── ctl.erb
│   │       ├── config.json.erb     # Broker config (plans, BOSH creds)
│   │       └── bpm.yml.erb
│   ├── openclaw-registry/
│   │   ├── spec
│   │   ├── monit
│   │   └── templates/
│   │       ├── ctl.erb
│   │       ├── allowlist.yml.erb   # Approved skills
│   │       └── sync.erb            # ClawHub sync script
│   ├── openclaw-admin-dashboard/
│   │   ├── spec
│   │   ├── monit
│   │   └── templates/
│   │       ├── ctl.erb
│   │       └── config.json.erb
│   ├── openclaw-sso-proxy/
│   │   ├── spec
│   │   ├── monit
│   │   └── templates/
│   │       ├── ctl.erb
│   │       ├── oauth2-proxy.cfg.erb  # Per-instance SSO config
│   │       └── bpm.yml.erb
│   ├── route-registrar/
│   │   ├── spec
│   │   ├── monit
│   │   └── templates/
│   │       ├── ctl.erb
│   │       └── registrar.json.erb    # Per-instance route definition
│   ├── register-broker/
│   │   ├── spec
│   │   └── templates/
│   │       └── run.erb
│   ├── deregister-broker/
│   │   ├── spec
│   │   └── templates/
│   │       └── run.erb
│   ├── smoke-tests/
│   │   ├── spec
│   │   └── templates/
│   │       └── run.erb
│   └── upgrade-agents/
│       ├── spec
│       └── templates/
│           └── run.erb
├── packages/
│   ├── node22/
│   │   ├── spec
│   │   └── packaging                # Compile Node.js 22 from source
│   ├── openclaw/
│   │   ├── spec
│   │   └── packaging                # npm install openclaw, bundle skills
│   ├── openclaw-broker/
│   │   ├── spec
│   │   └── packaging                # Go service broker binary
│   ├── openclaw-admin-dashboard/
│   │   ├── spec
│   │   └── packaging                # Dashboard web app
│   ├── oauth2-proxy/
│   │   ├── spec
│   │   └── packaging                # Per-instance SSO proxy binary
│   └── golang-1.22/
│       ├── spec
│       └── packaging
├── src/
│   ├── openclaw-broker/              # Go OSB implementation
│   │   ├── main.go
│   │   ├── broker/
│   │   │   ├── broker.go            # OSB API handlers
│   │   │   ├── provisioner.go       # BOSH CPI integration
│   │   │   ├── binding.go           # Credential generation
│   │   │   └── catalog.go           # Plan definitions
│   │   ├── bosh/
│   │   │   ├── client.go            # BOSH Director API client
│   │   │   ├── manifest.go          # Agent manifest generator
│   │   │   └── lifecycle.go         # Deploy/delete/upgrade ops
│   │   ├── security/
│   │   │   ├── token.go             # Gateway token generation
│   │   │   ├── seed.go              # Ed25519 seed/keypair derivation
│   │   │   └── policy.go            # Skills allowlist enforcement
│   │   └── go.mod
│   └── openclaw-admin-dashboard/     # Dashboard web app
│       ├── main.go
│       ├── handlers/
│       └── static/
├── manifests/
│   ├── openclaw.yml                  # Full multi-VM manifest
│   └── openclaw-lite.yml             # Minimal (broker only, no dashboard)
├── tile/
│   └── metadata/
│       └── openclaw.yml              # Ops Manager tile metadata
├── resources/
│   └── icon.png                      # Tile icon (lobster!)
├── scripts/
│   ├── add-blob.sh                   # Download + add OpenClaw blob
│   ├── build-tile.sh                 # Production tile build
│   └── build-tile-dev.sh             # Dev tile build (offline)
├── tile.yml                          # Tile Generator config
├── .gitignore
└── README.md
```

---

## 4. Job Specifications

### 4.1 `openclaw-agent`

The core job — a dedicated OpenClaw gateway running on its own VM with colocated node, WebChat UI, route registration, and optional SSO.

```yaml
# jobs/openclaw-agent/spec
---
name: openclaw-agent

templates:
  ctl.erb: bin/ctl
  pre-start.erb: bin/pre-start
  drain.erb: bin/drain
  bpm.yml.erb: config/bpm.yml
  openclaw.json.erb: config/openclaw.json
  skills-allowlist.yml.erb: config/skills-allowlist.yml
  security-policy.json.erb: config/security-policy.json

packages:
  - node22
  - openclaw

properties:
  openclaw.version:
    description: "Pinned OpenClaw version (must be >= 2026.1.29 for CVE-2026-25253 patch)"
    default: "2026.2.10"

  # Gateway
  openclaw.gateway.port:
    description: "Gateway WebSocket port"
    default: 18789
  openclaw.gateway.token:
    description: "Authentication token for gateway access"
  openclaw.gateway.bind_address:
    description: "Address to bind the gateway"
    default: "0.0.0.0"

  # WebChat UI
  openclaw.webchat.enabled:
    description: "Enable the WebChat UI"
    default: true
  openclaw.webchat.port:
    description: "WebChat HTTP port"
    default: 8080

  # Control UI (disabled by default — CVE-2026-25253 attack vector)
  openclaw.control_ui.enabled:
    description: "Enable the Control UI"
    default: false
  openclaw.control_ui.require_auth:
    description: "Require authentication for Control UI"
    default: true

  # LLM Configuration
  openclaw.llm.provider:
    description: "LLM provider type: genai, anthropic, openai, ollama, custom"
    default: "genai"
  openclaw.llm.genai.endpoint:
    description: "GenAI proxy endpoint URL"
  openclaw.llm.genai.api_key:
    description: "GenAI proxy API key"
  openclaw.llm.genai.model:
    description: "Model to use from GenAI service"
    default: "auto"
  openclaw.llm.anthropic.api_key:
    description: "Anthropic API key (alternative to GenAI)"
  openclaw.llm.openai.api_key:
    description: "OpenAI API key (alternative to GenAI)"
  openclaw.llm.model_override:
    description: "Force a specific model regardless of provider"

  # Security
  openclaw.security.sandbox_mode:
    description: "Sandbox enforcement: strict, moderate, loose"
    default: "strict"
  openclaw.security.skills_allowlist:
    description: "List of approved skill names from the registry"
    default: []
  openclaw.security.block_outbound_network:
    description: "Block outbound internet from the agent VM (except LLM endpoint)"
    default: false
  openclaw.security.max_shell_timeout_seconds:
    description: "Maximum execution time for shell commands"
    default: 300
  openclaw.security.readonly_paths:
    description: "Paths the agent can read but not modify"
    default: []
  openclaw.security.blocked_commands:
    description: "Shell commands the agent is forbidden from running"
    default:
      - "rm -rf /"
      - "dd if=/dev/zero"
      - "mkfs"
      - ":(){ :|:& };:"

  # Persistence
  openclaw.state_dir:
    description: "Directory for persistent state (on persistent disk)"
    default: "/var/vcap/store/openclaw"
  openclaw.memory.enabled:
    description: "Enable persistent memory across sessions"
    default: true

  # Node (system.run)
  openclaw.node.enabled:
    description: "Enable the execution node (colocated on same VM)"
    default: true
  openclaw.node.seed:
    description: "Shared seed for gateway-node keypair derivation"

  # Browser automation
  openclaw.browser.enabled:
    description: "Enable headless Chromium"
    default: false
  openclaw.browser.sandbox:
    description: "Run browser in strict sandbox mode"
    default: true

  # Skills registry
  openclaw.registry.endpoint:
    description: "URL of the curated skills registry"
  openclaw.registry.sync_interval_hours:
    description: "How often to sync skills from registry"
    default: 24

  # Channels
  openclaw.channels.webchat.enabled:
    description: "Enable WebChat channel"
    default: true
  openclaw.channels.slack.enabled:
    description: "Enable Slack channel"
    default: false
  openclaw.channels.slack.bot_token:
    description: "Slack bot token"
  openclaw.channels.teams.enabled:
    description: "Enable Microsoft Teams channel"
    default: false
  openclaw.channels.teams.webhook_url:
    description: "Teams incoming webhook URL"

  # Monitoring
  openclaw.metrics.enabled:
    description: "Enable Prometheus metrics endpoint"
    default: true
  openclaw.metrics.port:
    description: "Metrics port"
    default: 9400
  openclaw.logging.level:
    description: "Log level: debug, info, warn, error"
    default: "info"

  # Instance identity (set by broker)
  openclaw.instance.id:
    description: "Unique instance ID"
  openclaw.instance.owner:
    description: "Developer/team who owns this instance"
  openclaw.instance.plan:
    description: "Service plan used to provision"

  # Per-instance route registration
  openclaw.route.hostname:
    description: "Route hostname (e.g., openclaw-alice)"
  openclaw.route.domain:
    description: "Apps domain (e.g., apps.example.com)"
  openclaw.route.tls.enabled:
    description: "TLS termination at Go Router"
    default: true

  # Per-instance SSO
  openclaw.sso.enabled:
    description: "Enable SSO for this instance's WebChat UI"
    default: false
  openclaw.sso.provider:
    description: "SSO provider: p-identity, uaa, generic-oidc"
    default: "p-identity"
  openclaw.sso.client_id:
    description: "OAuth2 client ID"
  openclaw.sso.client_secret:
    description: "OAuth2 client secret"
  openclaw.sso.issuer_url:
    description: "OIDC issuer URL"
  openclaw.sso.cookie_secret:
    description: "Base64-encoded secret for session cookies"
  openclaw.sso.allowed_emails:
    description: "Emails allowed to access this instance (empty = all org members)"
    default: []
```

### 4.2 `openclaw-broker`

```yaml
# jobs/openclaw-broker/spec
---
name: openclaw-broker

templates:
  ctl.erb: bin/ctl
  config.json.erb: config/config.json
  bpm.yml.erb: config/bpm.yml
  catalog.json.erb: config/catalog.json

packages:
  - openclaw-broker
  - golang-1.22

properties:
  openclaw.broker.port:
    description: "Broker API port"
    default: 8080
  openclaw.broker.auth.username:
    description: "Basic auth username"
    default: "openclaw-broker"
  openclaw.broker.auth.password:
    description: "Basic auth password"
  openclaw.broker.tls.enabled:
    description: "Enable TLS"
    default: false
  openclaw.broker.tls.certificate:
    description: "TLS certificate PEM"
  openclaw.broker.tls.private_key:
    description: "TLS private key PEM"

  # BOSH Director connection
  openclaw.broker.bosh.director_url:
    description: "BOSH Director API URL"
  openclaw.broker.bosh.client_id:
    description: "BOSH UAA client ID"
  openclaw.broker.bosh.client_secret:
    description: "BOSH UAA client secret"
  openclaw.broker.bosh.ca_cert:
    description: "BOSH Director CA certificate"

  # Agent defaults
  openclaw.broker.agent_defaults.openclaw_version:
    description: "Default OpenClaw version for new instances"
    default: "2026.2.10"
  openclaw.broker.agent_defaults.stemcell:
    description: "Stemcell for agent VMs"
    default: "ubuntu-jammy"
  openclaw.broker.agent_defaults.network:
    description: "BOSH network for agent VMs"
    default: "openclaw-agents"
  openclaw.broker.agent_defaults.az:
    description: "Availability zone for agent VMs"

  # GenAI defaults (inherited by agents)
  openclaw.broker.genai.endpoint:
    description: "Default GenAI proxy endpoint"
  openclaw.broker.genai.api_key:
    description: "Default GenAI API key"
  openclaw.broker.genai.model:
    description: "Default model"

  # Plan definitions
  openclaw.broker.catalog.plans:
    description: "Service plan configurations"
    default:
      - name: developer
        id: "openclaw-developer-plan"
        description: "Dedicated OpenClaw agent for individual developers"
        vm_type: small
        disk_type: "10GB"
        memory: 2048
        metadata:
          displayName: "Developer"
          bullets:
            - "Dedicated VM with isolated WebChat UI"
            - "2GB RAM, 10GB persistent disk"
            - "Cloud LLM integration"
            - "Per-instance SSO"
      - name: developer-plus
        id: "openclaw-developer-plus-plan"
        description: "Enhanced agent with browser automation"
        vm_type: medium
        disk_type: "20GB"
        memory: 4096
        features:
          browser: true
        metadata:
          displayName: "Developer Plus"
          bullets:
            - "Dedicated VM with isolated WebChat UI"
            - "4GB RAM, 20GB persistent disk"
            - "Browser automation enabled"
            - "All messaging channels"
      - name: team
        id: "openclaw-team-plan"
        description: "Shared agent for teams"
        vm_type: large
        disk_type: "50GB"
        memory: 8192
        features:
          browser: true
          multi_user: true
        metadata:
          displayName: "Team"
          bullets:
            - "Dedicated VM with isolated WebChat UI"
            - "8GB RAM, 50GB persistent disk"
            - "Multi-user with Slack/Teams"
            - "Full browser automation"

  # Metering
  openclaw.broker.metering.enabled:
    description: "Enable token usage metering per instance"
    default: true
  openclaw.broker.metering.export_interval_minutes:
    description: "Usage metrics export interval"
    default: 60

  # Limits
  openclaw.broker.limits.max_instances:
    description: "Maximum total agent instances"
    default: 50
  openclaw.broker.limits.max_instances_per_org:
    description: "Maximum instances per CF org"
    default: 10
```

### 4.3 `openclaw-registry`

```yaml
# jobs/openclaw-registry/spec
---
name: openclaw-registry

templates:
  ctl.erb: bin/ctl
  allowlist.yml.erb: config/allowlist.yml
  sync.erb: bin/sync
  bpm.yml.erb: config/bpm.yml

packages:
  - node22
  - openclaw

properties:
  openclaw.registry.port:
    description: "Registry API port"
    default: 8081
  openclaw.registry.skills_allowlist:
    description: "Approved skills from ClawHub"
    default:
      - weather
      - calendar-google
      - calendar-apple
      - email-gmail
      - email-outlook
      - github
      - jira
      - confluence
      - slack-actions
      - notion
      - obsidian
      - trello
      - linear
  openclaw.registry.auto_sync:
    description: "Auto-sync approved skills from ClawHub"
    default: true
  openclaw.registry.sync_interval_hours:
    description: "Sync interval"
    default: 24
  openclaw.registry.scan_on_sync:
    description: "Security scan skills during sync"
    default: true
  openclaw.registry.block_network_calls:
    description: "Block skills that make outbound HTTP calls to unknown domains"
    default: true
  openclaw.registry.storage_dir:
    description: "Skills storage directory"
    default: "/var/vcap/store/openclaw-registry"
```

### 4.4 Errands

```yaml
# jobs/register-broker/spec
---
name: register-broker
templates:
  run.erb: bin/run
packages: []
properties:
  cf.api_url:
    description: "Cloud Foundry API URL"
  cf.admin_username:
    description: "CF admin username"
  cf.admin_password:
    description: "CF admin password"
  cf.skip_ssl_validation:
    description: "Skip SSL validation"
    default: false
  openclaw.broker.route:
    description: "Broker route hostname"
  openclaw.broker.auth.username:
    description: "Broker basic auth username"
  openclaw.broker.auth.password:
    description: "Broker basic auth password"

---
# jobs/smoke-tests/spec
---
name: smoke-tests
templates:
  run.erb: bin/run
packages:
  - openclaw-broker
properties:
  openclaw.smoke_tests.org:
    description: "CF org for smoke tests"
    default: "system"
  openclaw.smoke_tests.space:
    description: "CF space for smoke tests"
    default: "openclaw-smoke-tests"
  openclaw.smoke_tests.plan:
    description: "Plan to test"
    default: "developer"
  openclaw.smoke_tests.timeout_seconds:
    description: "Timeout for agent provisioning"
    default: 600
  openclaw.smoke_tests.verify_webchat_route:
    description: "Verify the agent's dedicated WebChat URL is reachable after provisioning"
    default: true
  openclaw.smoke_tests.test_prompt:
    description: "Prompt to send to verify LLM connectivity"
    default: "What is 2+2? Reply with just the number."

---
# jobs/upgrade-agents/spec
---
name: upgrade-agents
templates:
  run.erb: bin/run
packages:
  - openclaw-broker
properties:
  openclaw.upgrade.target_version:
    description: "OpenClaw version to upgrade all agents to"
  openclaw.upgrade.canary_percentage:
    description: "Percentage of instances to upgrade first as canary"
    default: 10
  openclaw.upgrade.pause_after_canary:
    description: "Pause after canary for manual verification"
    default: true
  openclaw.upgrade.max_parallel:
    description: "Maximum parallel upgrades"
    default: 5
```

---

## 5. Packages

### 5.1 `node22`

```yaml
# packages/node22/spec
---
name: node22
files:
  - node/node-v22.*.tar.gz
```

```bash
# packages/node22/packaging
set -e
tar xzf node/node-v22.*.tar.gz
NODE_DIR=$(ls -d node-v22.*)
cp -a ${NODE_DIR}/* ${BOSH_INSTALL_TARGET}/
```

### 5.2 `openclaw`

```yaml
# packages/openclaw/spec
---
name: openclaw
dependencies:
  - node22
files:
  - openclaw/openclaw-*.tgz
  - openclaw/skills-bundle-*.tgz
```

```bash
# packages/openclaw/packaging
set -e
export PATH=${BOSH_PACKAGES_DIR}/node22/bin:$PATH
export HOME=/var/vcap

mkdir -p ${BOSH_INSTALL_TARGET}/lib
cd ${BOSH_INSTALL_TARGET}/lib

npm install --global-style --no-optional ${BOSH_COMPILE_TARGET}/openclaw/openclaw-*.tgz

tar xzf ${BOSH_COMPILE_TARGET}/openclaw/skills-bundle-*.tgz \
  -C ${BOSH_INSTALL_TARGET}/lib/skills/

mkdir -p ${BOSH_INSTALL_TARGET}/bin
cat > ${BOSH_INSTALL_TARGET}/bin/openclaw <<'EOF'
#!/bin/bash
export PATH=/var/vcap/packages/node22/bin:$PATH
exec node /var/vcap/packages/openclaw/lib/node_modules/openclaw/dist/cli.js "$@"
EOF
chmod +x ${BOSH_INSTALL_TARGET}/bin/openclaw
```

### 5.3 `openclaw-broker`

```yaml
# packages/openclaw-broker/spec
---
name: openclaw-broker
dependencies:
  - golang-1.22
files:
  - openclaw-broker/**/*
```

```bash
# packages/openclaw-broker/packaging
set -e
export GOROOT=${BOSH_PACKAGES_DIR}/golang-1.22
export PATH=${GOROOT}/bin:$PATH
export GOPATH=${BOSH_COMPILE_TARGET}/go

mkdir -p ${GOPATH}/src/github.com/nkuhn-vmw/bosh-openclaw
cp -a ${BOSH_COMPILE_TARGET}/openclaw-broker/* \
  ${GOPATH}/src/github.com/nkuhn-vmw/bosh-openclaw/

cd ${GOPATH}/src/github.com/nkuhn-vmw/bosh-openclaw
go build -o ${BOSH_INSTALL_TARGET}/bin/openclaw-broker .
```

---

## 6. Service Broker Implementation

### 6.1 Broker API Endpoints

The Go broker implements Open Service Broker API v2.16:

| Endpoint | Method | Description |
|---|---|---|
| `/v2/catalog` | GET | Returns available plans |
| `/v2/service_instances/:id` | PUT | Provisions a new agent VM via BOSH |
| `/v2/service_instances/:id` | DELETE | Deprovisions agent VM and cleans up |
| `/v2/service_instances/:id` | PATCH | Updates agent configuration |
| `/v2/service_instances/:id/service_bindings/:bid` | PUT | Creates binding with gateway credentials |
| `/v2/service_instances/:id/service_bindings/:bid` | DELETE | Revokes binding credentials |
| `/v2/service_instances/:id/last_operation` | GET | Async provisioning status |

### 6.2 Provisioning Logic

When the broker receives a provision request:

1. Validates against plan limits (max instances, max per org)
2. Generates unique gateway token and node seed via crypto-secure random
3. Derives a route hostname from the owner identity (e.g., `openclaw-alice`)
4. If SSO is enabled, creates a `p-identity` service instance and extracts OAuth2 client credentials
5. Constructs a BOSH deployment manifest for a single-VM `openclaw-agent` deployment, including colocated `route-registrar` and optional `openclaw-sso-proxy` jobs
6. Submits the manifest to the BOSH Director via API
7. Polls for deployment completion (async operation with `last_operation`)
8. Verifies the WebChat route is reachable through the Go Router
9. Returns the dedicated WebChat URL and gateway credentials

On **deprovisioning**:

1. Deletes the BOSH deployment (stops the agent, tears down route-registrar, deregisters the route)
2. Cleans up the `p-identity` service instance if SSO was configured
3. Removes the instance record from the broker's state

### 6.3 Agent VM Manifest Template

The broker dynamically generates this manifest for each provisioned agent. `route-registrar` and `openclaw-sso-proxy` are colocated on the agent VM so each instance owns its own route and auth.

```yaml
# Generated by openclaw-broker for instance {{ .InstanceID }}
---
name: openclaw-agent-{{ .InstanceID }}

instance_groups:
  - name: agent
    instances: 1
    jobs:
      - name: openclaw-agent
        release: openclaw
        properties:
          openclaw:
            version: {{ .OpenClawVersion }}
            gateway:
              token: {{ .GatewayToken }}
              port: 18789
            webchat:
              enabled: true
              port: {{ if .SSOEnabled }}8081{{ else }}8080{{ end }}
            control_ui:
              enabled: {{ .ControlUIEnabled }}
              require_auth: true
            llm:
              provider: {{ .LLMProvider }}
              genai:
                endpoint: {{ .GenAIEndpoint }}
                api_key: {{ .GenAIAPIKey }}
                model: {{ .Model }}
            security:
              sandbox_mode: {{ .SandboxMode }}
              skills_allowlist: {{ .SkillsAllowlist }}
              blocked_commands: {{ .BlockedCommands }}
            node:
              enabled: true
              seed: {{ .NodeSeed }}
            browser:
              enabled: {{ .BrowserEnabled }}
            registry:
              endpoint: {{ .RegistryEndpoint }}
            instance:
              id: {{ .InstanceID }}
              owner: {{ .Owner }}
              plan: {{ .PlanName }}
            route:
              hostname: {{ .RouteHostname }}
              domain: {{ .AppsDomain }}
            sso:
              enabled: {{ .SSOEnabled }}
            state_dir: /var/vcap/store/openclaw

      # Dedicated route for this instance's WebChat UI
      - name: route-registrar
        release: openclaw
        properties:
          route_registrar:
            routes:
              - name: openclaw-{{ .InstanceID }}
                registration_interval: 20s
                port: 8080
                uris:
                  - {{ .RouteHostname }}.{{ .AppsDomain }}
                health_check:
                  name: openclaw-webchat
                  script_path: /var/vcap/jobs/route-registrar/bin/health-check.sh
            nats:
              machines: {{ .NATSMachines }}
              port: {{ .NATSPort }}
              user: {{ .NATSUser }}
              password: {{ .NATSPassword }}

      {{ if .SSOEnabled }}
      # Per-instance SSO proxy (oauth2-proxy → WebChat)
      - name: openclaw-sso-proxy
        release: openclaw
        properties:
          openclaw:
            sso:
              enabled: true
              listen_port: 8080
              upstream_port: 8081
              client_id: {{ .SSOClientID }}
              client_secret: {{ .SSOClientSecret }}
              issuer_url: {{ .SSOIssuerURL }}
              cookie_secret: {{ .SSOCookieSecret }}
              redirect_url: https://{{ .RouteHostname }}.{{ .AppsDomain }}/oauth2/callback
              allowed_emails: {{ .SSOAllowedEmails }}
      {{ end }}

    vm_type: {{ .VMType }}
    stemcell: default
    azs: {{ .AZs }}
    persistent_disk_type: {{ .DiskType }}
    networks:
      - name: {{ .Network }}

stemcells:
  - alias: default
    os: ubuntu-jammy
    version: latest

releases:
  - name: openclaw
    version: latest

update:
  canaries: 1
  max_in_flight: 1
  canary_watch_time: 30000-120000
  update_watch_time: 30000-120000
```

**Port layout per agent VM:**

| Port | Process | External? | Description |
|---|---|---|---|
| 8080 | SSO proxy (if enabled) or WebChat (if no SSO) | Yes (via Go Router) | Route-registered port; what the developer hits |
| 8081 | WebChat UI | No (localhost only when SSO enabled) | Upstream for oauth2-proxy |
| 18789 | Gateway WebSocket | No (internal network) | Programmatic access via `cf bind-service` credentials |
| 9400 | Prometheus metrics | No (scrape target) | Agent metrics export |

### 6.4 Binding Credentials

When a CF app binds to an OpenClaw service instance, the broker returns credentials including the instance's dedicated WebChat URL:

```json
{
  "credentials": {
    "webchat_url": "https://openclaw-alice.apps.example.com",
    "gateway_url": "wss://openclaw-agent-abc123.agents.internal:18789",
    "gateway_token": "oc_tok_a1b2c3d4e5f6...",
    "api_endpoint": "https://openclaw-alice.apps.example.com/api",
    "instance_id": "abc123",
    "owner": "alice@example.com",
    "plan": "developer",
    "openclaw_version": "2026.2.10",
    "node_seed": "seed_x9y8z7...",
    "sso_enabled": true
  }
}
```

The `webchat_url` resolves through the CF Go Router directly to that specific VM. The `gateway_url` uses an internal BOSH network address for programmatic WebSocket access from bound CF apps.

---

## 7. Tanzu Ops Manager Tile

### 7.1 `tile.yml`

```yaml
---
name: openclaw
icon_file: resources/icon.png
label: OpenClaw AI Agent Platform
description: |
  Deploy dedicated OpenClaw AI agents for developers and teams.
  Each agent runs on an isolated VM with its own WebChat UI,
  enterprise security controls, centralized LLM integration,
  and curated skills management.
metadata_version: '2.0'

stemcell_criteria:
  os: ubuntu-jammy
  requires_cpi: false
  version: '1.*'

releases:
  - name: openclaw
    file: openclaw-release.tgz
    version: '0.1.0'

forms:
  - name: genai-config
    label: GenAI / LLM Configuration
    description: Configure the LLM provider for all OpenClaw agents
    properties:
      - name: genai_provider
        type: selector
        label: LLM Provider
        configurable: true
        default: "Tanzu GenAI Service"
        option_templates:
          - name: tanzu_genai
            select_value: "Tanzu GenAI Service"
            property_blueprints:
              - name: endpoint
                type: string
                label: GenAI Proxy Endpoint
                configurable: true
              - name: api_key
                type: secret
                label: GenAI API Key
                configurable: true
              - name: default_model
                type: string
                label: Default Model
                configurable: true
                default: "auto"
          - name: anthropic_direct
            select_value: "Anthropic (Direct)"
            property_blueprints:
              - name: api_key
                type: secret
                label: Anthropic API Key
                configurable: true
              - name: model
                type: dropdown_select
                label: Model
                configurable: true
                default: "claude-sonnet-4-5-20250929"
                options:
                  - name: "claude-opus-4-6"
                    label: "Claude Opus 4.6"
                  - name: "claude-sonnet-4-5-20250929"
                    label: "Claude Sonnet 4.5"
                  - name: "claude-haiku-4-5-20251001"
                    label: "Claude Haiku 4.5"
          - name: openai_direct
            select_value: "OpenAI (Direct)"
            property_blueprints:
              - name: api_key
                type: secret
                label: OpenAI API Key
                configurable: true
              - name: model
                type: string
                label: Model
                configurable: true
                default: "gpt-4o"

  - name: security-config
    label: Security & Compliance
    description: Enterprise security controls for all agent instances
    properties:
      - name: sandbox_mode
        type: dropdown_select
        label: Sandbox Mode
        configurable: true
        default: "strict"
        options:
          - name: strict
            label: "Strict — All commands require explicit approval"
          - name: moderate
            label: "Moderate — Common commands allowed, destructive blocked"
          - name: loose
            label: "Loose — Most commands allowed (not recommended)"
      - name: control_ui_enabled
        type: boolean
        label: Enable Control UI
        configurable: true
        default: false
        description: "WARNING: The Control UI was the attack vector for CVE-2026-25253. Enable only on patched versions with auth required."
      - name: browser_automation_default
        type: boolean
        label: Enable Browser Automation by Default
        configurable: true
        default: false
      - name: block_outbound_network
        type: boolean
        label: Block Outbound Network (except LLM)
        configurable: true
        default: false
      - name: blocked_commands
        type: text
        label: Blocked Shell Commands
        configurable: true
        default: "rm -rf /\ndd if=/dev/zero\nmkfs\ncurl\nwget"
        description: "One command per line."
      - name: min_openclaw_version
        type: string
        label: Minimum OpenClaw Version
        configurable: true
        default: "2026.1.29"
        description: "Refuse to deploy agents below this version"

  - name: skills-config
    label: Skills Management
    description: Control which skills are available to agents
    properties:
      - name: skills_mode
        type: dropdown_select
        label: Skills Policy
        configurable: true
        default: "allowlist"
        options:
          - name: allowlist
            label: "Allowlist — Only approved skills"
          - name: blocklist
            label: "Blocklist — All except blocked"
          - name: disabled
            label: "Disabled — Built-in tools only"
      - name: approved_skills
        type: text
        label: Approved Skills (one per line)
        configurable: true
        default: |
          weather
          calendar-google
          email-gmail
          github
          jira
          slack-actions
          notion
      - name: auto_sync_from_clawhub
        type: boolean
        label: Auto-sync from ClawHub
        configurable: true
        default: true
      - name: scan_skills_on_sync
        type: boolean
        label: Security Scan Skills on Sync
        configurable: true
        default: true

  - name: sso-config
    label: SSO / Authentication
    description: Configure Single Sign-On for agent WebChat instances
    properties:
      - name: sso_enabled
        type: boolean
        label: Enable SSO for Agent Instances
        configurable: true
        default: true
        description: "Each agent's WebChat UI is protected by SSO via p-identity."
      - name: sso_plan
        type: string
        label: p-identity Service Plan
        configurable: true
        default: "uaa"
      - name: sso_allowed_email_domains
        type: text
        label: Allowed Email Domains
        configurable: true
        description: "One per line (e.g., example.com). Empty = all authenticated users."
      - name: sso_session_timeout_hours
        type: integer
        label: Session Timeout (hours)
        configurable: true
        default: 8

  - name: broker-config
    label: Service Broker
    description: Configure the OpenClaw service broker
    properties:
      - name: broker_username
        type: string
        label: Broker Username
        configurable: true
        default: "openclaw-broker"
      - name: broker_password
        type: secret
        label: Broker Password
        configurable: true
      - name: enable_developer_plan
        type: boolean
        label: Enable Developer Plan
        configurable: true
        default: true
      - name: enable_developer_plus_plan
        type: boolean
        label: Enable Developer Plus Plan
        configurable: true
        default: true
      - name: enable_team_plan
        type: boolean
        label: Enable Team Plan
        configurable: true
        default: false
      - name: max_instances
        type: integer
        label: Maximum Total Instances
        configurable: true
        default: 50
      - name: max_instances_per_org
        type: integer
        label: Maximum Instances Per Org
        configurable: true
        default: 10

  - name: networking-config
    label: Networking
    description: Route and network configuration
    properties:
      - name: apps_domain
        type: string
        label: Apps Domain
        configurable: true
        description: "Each agent gets a route like openclaw-<owner>.apps.example.com"
      - name: route_prefix
        type: string
        label: Agent Route Prefix
        configurable: true
        default: "openclaw"
      - name: broker_route
        type: string
        label: Broker Route Hostname
        configurable: true
        default: "openclaw-broker"
      - name: dashboard_route
        type: string
        label: Admin Dashboard Route Hostname
        configurable: true
        default: "openclaw-admin"
      - name: agent_network
        type: string
        label: Agent VM Network
        configurable: true
        default: "openclaw-agents"

# Static infrastructure
instance_groups:
  - name: openclaw-broker
    instances: 1
    jobs:
      - name: openclaw-broker
        release: openclaw
      - name: route-registrar
        release: openclaw
    vm_type: small
    stemcell: default
    azs: [z1]
    networks:
      - name: default

  - name: openclaw-registry
    instances: 1
    jobs:
      - name: openclaw-registry
        release: openclaw
    vm_type: small
    stemcell: default
    azs: [z1]
    persistent_disk_type: "10GB"
    networks:
      - name: default

  - name: openclaw-admin-dashboard
    instances: 1
    jobs:
      - name: openclaw-admin-dashboard
        release: openclaw
      - name: route-registrar
        release: openclaw
    vm_type: small
    stemcell: default
    azs: [z1]
    networks:
      - name: default

post_deploy_errands:
  - name: register-broker
  - name: smoke-tests

pre_delete_errands:
  - name: deregister-broker
```

---

## 8. Security

### 8.1 Defense-in-Depth

```
Layer 1: Network Isolation
├── Agent VMs on dedicated BOSH network
├── Only LLM endpoint whitelisted in outbound firewall
├── No direct ingress — all access via CF Go Router
└── Inter-agent communication blocked

Layer 2: Version Enforcement
├── Tile refuses to deploy versions < 2026.1.29
├── Upgrade-agents errand for fleet-wide patching
└── Version pinning in broker manifest templates

Layer 3: Control UI Lockdown
├── Disabled by default (CVE-2026-25253 attack vector)
├── When enabled, requires authentication
└── Origin header validation enforced

Layer 4: Per-Instance SSO
├── Each agent VM runs its own oauth2-proxy
├── SSO redirect URIs scoped to that instance's route
├── No shared session store across instances
├── Session timeout enforced at tile level
└── Email domain restriction supported

Layer 5: Skills Vetting
├── Registry scans skills for outbound HTTP calls
├── Allowlist-only mode (default)
├── Data exfiltration pattern detection
└── Skills cannot install without registry approval

Layer 6: Runtime Sandbox
├── Sandbox mode configurable per-tile (strict/moderate/loose)
├── Blocked commands list enforced at OS level
├── Shell timeout enforcement
└── Persistent disk isolation per instance

Layer 7: Credential Management
├── All tokens generated with crypto-secure random
├── Ed25519 keypairs derived from seeds (no key storage)
├── CredHub integration for secret storage
└── Token rotation via broker update operation
```

### 8.2 CVE-2026-25253 Mitigations

CVE-2026-25253 (CVSS 8.8) is a 1-click RCE via WebSocket token exfiltration through the Control UI. The tile implements:

1. **Version gate** — broker refuses to provision agents running versions prior to 2026.1.29
2. **Control UI disabled** — the WebSocket-based Control UI is off by default
3. **Origin validation** — when Control UI is enabled, strict `Origin` header validation
4. **Network isolation** — agent VMs are not internet-reachable; attacker C2 servers cannot receive exfiltrated tokens
5. **Token scoping** — gateway tokens are scoped per-instance and cannot be reused across agents

### 8.3 Supply Chain (Skills)

The tile addresses malicious skills supply chain risks via:

1. **Allowlist-only mode** (default) — only operator-approved skills can be installed
2. **Security scanning on sync** — skills are scanned for outbound HTTP calls, fetch/axios/curl patterns, and data exfiltration indicators
3. **Centralized registry** — the `openclaw-registry` VM hosts a curated local mirror; agents pull from it, not directly from ClawHub
4. **Network blocking** — optional outbound network blocking prevents skills from phoning home even if they bypass the scan

---

## 9. Monitoring & Operations

### 9.1 Prometheus Metrics

| Component | Metric | Description |
|---|---|---|
| `openclaw-broker` | `openclaw_broker_instances_total` | Total provisioned instances |
| `openclaw-broker` | `openclaw_broker_provision_duration_seconds` | Provisioning time histogram |
| `openclaw-agent` | `openclaw_agent_tokens_consumed_total` | LLM tokens consumed |
| `openclaw-agent` | `openclaw_agent_sessions_active` | Active chat sessions |
| `openclaw-agent` | `openclaw_agent_skills_invoked_total` | Skill invocations by name |
| `openclaw-agent` | `openclaw_agent_shell_commands_total` | Shell commands executed |
| `openclaw-agent` | `openclaw_agent_uptime_seconds` | Agent uptime |
| `openclaw-registry` | `openclaw_registry_skills_total` | Total approved skills |
| `openclaw-registry` | `openclaw_registry_sync_last_success` | Last successful sync timestamp |

### 9.2 Health Monitoring

The `openclaw-agent` monit configuration:

- Monitors the Node.js gateway process
- Restarts on crash (BOSH resurrection)
- Checks WebSocket port availability
- Validates LLM endpoint connectivity on startup
- Route-registrar health check deregisters unhealthy agents from Go Router

### 9.3 Audit Logging

All agent activity logs to syslog with structured fields:

```
openclaw.agent.abc123 | action=shell_exec | command="ls -la" | user=alice | result=success
openclaw.agent.abc123 | action=skill_invoke | skill=github | user=alice | result=success
openclaw.agent.abc123 | action=llm_request | model=claude-sonnet-4.5 | tokens=1523 | user=alice
openclaw.broker       | action=provision | instance=abc123 | plan=developer | org=dev-team
```

---

## 10. Developer Experience

### 10.1 Self-Service via CF CLI

```bash
# Browse available plans
cf marketplace -e openclaw

# Provision a personal agent
cf create-service openclaw developer my-agent

# Check provisioning status (async — VM takes ~2-3 min to boot)
cf service my-agent

# Get credentials including dedicated WebChat URL
cf create-service-key my-agent agent-key
cf service-key my-agent agent-key

# Bind to an app (credentials injected via VCAP_SERVICES)
cf bind-service my-app my-agent

# Upgrade to a bigger plan
cf update-service my-agent -p developer-plus

# Deprovision (tears down VM, deregisters route)
cf delete-service my-agent
```

### 10.2 Dedicated WebChat UI

Each developer gets their own dedicated URL:

```
https://openclaw-alice.apps.example.com
```

This is a fully isolated OpenClaw WebChat instance on a dedicated VM — their own chat history, memory, skills workspace, and session state. No other developer can see or access it.

If SSO is enabled, the developer is redirected to the organization's identity provider before reaching the WebChat UI. The oauth2-proxy runs colocated on the same VM — auth happens at the edge of the instance with no shared state.

The route is registered by the agent VM's own `route-registrar` job:
- Route appears when the VM boots
- Route deregisters when the instance is deprovisioned
- Health checks are per-instance (unhealthy agents stop receiving traffic)

### 10.3 Programmatic Access

Applications can interact with the agent via WebSocket:

```javascript
const WebSocket = require('ws');
const creds = JSON.parse(process.env.VCAP_SERVICES).openclaw[0].credentials;

const ws = new WebSocket(creds.gateway_url, {
  headers: { 'Authorization': `Bearer ${creds.gateway_token}` }
});

ws.on('open', () => {
  ws.send(JSON.stringify({
    type: 'chat',
    content: 'Summarize the latest PR reviews on our repo'
  }));
});
```

---

## 11. Build & Release

### 11.1 Scripts

```bash
# Download OpenClaw and Node.js blobs
./scripts/add-blob.sh 2026.2.10

# Create BOSH release
bosh create-release --force

# Upload to director
bosh upload-release

# Build Ops Manager tile (production — downloads deps)
./scripts/build-tile.sh

# Build tile for dev (no network required)
./scripts/build-tile-dev.sh
```

### 11.2 CI/CD Pipeline

The `.github/workflows/ci.yml` pipeline:

1. Checks out the repo
2. Downloads the latest OpenClaw npm package
3. Bundles approved skills from ClawHub
4. Creates BOSH release
5. Builds Ops Manager tile (`.pivotal` file)
6. Runs smoke tests against a BOSH-Lite environment
7. Publishes tile artifact to GitHub Releases

---

## 12. Open Questions / Future Work

1. **GPU Pass-through** — for users running local models (Ollama), should the tile support GPU-enabled VM types? Requires IaaS-specific stemcell extensions.

2. **Multi-Region** — should the broker support deploying agents across AZs for HA, or is single-VM-per-developer sufficient?

3. **Shared Skills Workspace** — should team-plan agents share a skills workspace, or should each team member have isolated skills?

4. **Token Budget Enforcement** — should the broker enforce per-instance LLM token budgets? Requires metering layer to intercept LLM API calls.

5. **Channels via Tile Config** — should Slack bot tokens, Teams webhooks be manageable via Ops Manager forms, or left to per-developer config via WebChat settings?

6. **Tanzu AI Services Provider** — the Goose AI agent provider work could complement this. Should the tile also register as a Tanzu AI Services provider?

---

## 13. References

- [OpenClaw GitHub Repository](https://github.com/openclaw/openclaw)
- [OpenClaw Documentation](https://docs.openclaw.ai)
- [CVE-2026-25253 Advisory](https://nvd.nist.gov/vuln/detail/CVE-2026-25253)
- [CrowdStrike: Security Teams & OpenClaw](https://www.crowdstrike.com/en-us/blog/what-security-teams-need-to-know-about-openclaw-ai-super-agent/)
- [Adversa.ai: OpenClaw Security Hardening Guide](https://adversa.ai/blog/openclaw-security-101-vulnerabilities-hardening-2026/)
- [`bosh-seaweedfs` Reference Architecture](https://github.com/nkuhn-vmw/bosh-seaweedfs)
- [Tanzu Ops Manager Tile Developer Guide](https://docs.vmware.com/en/VMware-Tanzu-Operations-Manager/3.0/tile-dev-guide/index.html)
