package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/sceptyre/maia/internal/config"
)

// settingsView is the admin-dash payload: LLM config (from ~/.maia/config.json)
// merged with web config (registry defaults + UI). Secrets are never echoed;
// only presence flags are returned (and "{cmd:...}" passthroughs, which are
// commands rather than secrets).
type settingsView struct {
	Config   configView `json:"config"`
	UI       UISettings `json:"ui"`
	Defaults Defaults   `json:"defaults"`
	Server   serverView `json:"server"`
}

type serverView struct {
	Version  string `json:"version"`
	Addr     string `json:"addr"`
	Port     int    `json:"port"`
	TokenSet bool   `json:"token_set"`
	Hostname string `json:"hostname"`
}

type configView struct {
	Model         string            `json:"model"`
	OpenAIBaseURL string            `json:"openai_base_url"`
	OpenAIHeaders map[string]string `json:"openai_headers"`
	OpenAIAPIKey  string            `json:"openai_api_key"` // "" | "set" | "{cmd:...}"
	BraveAPIKey   string            `json:"brave_api_key"`  // "" | "set" | "{cmd:...}"
	GithubToken   string            `json:"github_token"`   // "" | "set" | "{cmd:...}"`
	MaxTokens     int               `json:"max_tokens"`
	EnvOverride   bool              `json:"env_override"` // OPENAI_API_KEY present in env
}

// maskKey renders a secret for the UI: passthrough commands are safe to show.
func maskKey(v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "{cmd:") {
		return v
	}
	return "set"
}

func (s *Server) loadConfigView() configView {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	return configView{
		Model:         cfg.Model,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
		OpenAIHeaders: cfg.OpenAIHeaders,
		OpenAIAPIKey:  maskKey(cfg.OpenAIAPIKey),
		BraveAPIKey:   maskKey(cfg.BraveAPIKey),
		GithubToken:   maskKey(cfg.GithubToken),
		MaxTokens:     cfg.MaxTokens,
		EnvOverride:   os.Getenv("OPENAI_API_KEY") != "",
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	reg := s.registry.Get()
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, settingsView{
			Config:   s.loadConfigView(),
			UI:       reg.UI,
			Defaults: reg.Defaults,
			Server: serverView{
				Version:  s.Version,
				Addr:     s.Addr,
				Port:     s.Port,
				TokenSet: s.Token != "",
				Hostname: hostname(),
			},
		})
		return
	}
	if r.Method != http.MethodPut {
		writeErr(w, http.StatusMethodNotAllowed, "use GET or PUT")
		return
	}

	var in struct {
		Config   *configView `json:"config"`
		UI       *UISettings `json:"ui"`
		Defaults *Defaults   `json:"defaults"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// LLM config: only overwrite secrets when a new value is supplied.
	if in.Config != nil {
		cfg, err := config.Load()
		seenHeaders := make(map[string]bool)
		for name, value := range in.Config.OpenAIHeaders {
			canonical := http.CanonicalHeaderKey(name)
			if !validHTTPHeaderName(name) || seenHeaders[canonical] || strings.ContainsAny(value, "\r\n") {
				writeErr(w, http.StatusBadRequest, "custom headers must use unique valid names and single-line values")
				return
			}
			seenHeaders[canonical] = true
		}

		if err != nil || cfg == nil {
			cfg = &config.Config{}
		}
		cfg.Model = in.Config.Model
		cfg.OpenAIBaseURL = strings.TrimSpace(in.Config.OpenAIBaseURL)
		if in.Config.OpenAIHeaders != nil {
			cfg.OpenAIHeaders = in.Config.OpenAIHeaders
		}

		cfg.MaxTokens = in.Config.MaxTokens
		if in.Config.OpenAIAPIKey != "" && !strings.HasPrefix(in.Config.OpenAIAPIKey, "set") {
			cfg.OpenAIAPIKey = in.Config.OpenAIAPIKey
		}
		if in.Config.BraveAPIKey != "" && !strings.HasPrefix(in.Config.BraveAPIKey, "set") {
			cfg.BraveAPIKey = in.Config.BraveAPIKey
		}
		if in.Config.GithubToken != "" && !strings.HasPrefix(in.Config.GithubToken, "set") {
			cfg.GithubToken = in.Config.GithubToken
		}
		if err := config.Save(cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, "save config: "+err.Error())
			return
		}
	}

	if in.UI != nil || in.Defaults != nil {
		if err := s.registry.Update(func(reg *Registry) error {
			if in.UI != nil && in.UI.Theme != "" {
				reg.UI.Theme = in.UI.Theme
			}
			if in.Defaults != nil {
				if in.Defaults.MaxActiveWorktrees > 0 {
					reg.Defaults.MaxActiveWorktrees = in.Defaults.MaxActiveWorktrees
				}
				if in.Defaults.Github.ImportMode == "auto" || in.Defaults.Github.ImportMode == "manual" {
					reg.Defaults.Github.ImportMode = in.Defaults.Github.ImportMode
				}
				if in.Defaults.Github.PollIntervalMin > 0 {
					reg.Defaults.Github.PollIntervalMin = in.Defaults.Github.PollIntervalMin
				}
				if in.Defaults.Github.Labels != nil {
					reg.Defaults.Github.Labels = in.Defaults.Github.Labels
				}
				reg.Defaults.Github.Enabled = in.Defaults.Github.Enabled
				if in.Defaults.Github.Mode != "" {
					reg.Defaults.Github.Mode = in.Defaults.Github.Mode
				}
			}
			return nil
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	reg = s.registry.Get()
	writeJSON(w, http.StatusOK, settingsView{
		Config:   s.loadConfigView(),
		UI:       reg.UI,
		Defaults: reg.Defaults,
		Server: serverView{
			Version:  s.Version,
			Addr:     s.Addr,
			Port:     s.Port,
			TokenSet: s.Token != "",
			Hostname: hostname(),
		},
	})
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
