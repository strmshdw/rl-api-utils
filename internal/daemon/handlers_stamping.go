package daemon

import (
	"log/slog"
	"net/http"

	"github.com/dank/rl-api-utils/internal/config"
)

func (d *Daemon) stampingConfig() config.StampingConfig {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cfg == nil {
		return config.DefaultStampingConfig()
	}
	return d.cfg.EffectiveStampingConfig()
}

// handleGetStampingConfig handles GET /api/stamping/config, returning the active Option 5 stamping thresholds.
func (d *Daemon) handleGetStampingConfig(w http.ResponseWriter, r *http.Request) {
	d.writeJSONResponse(w, r, http.StatusOK, d.stampingConfig(), "error encoding stamping config response")
}

// handleUpdateStampingConfig handles POST /api/stamping/config and PATCH /api/stamping/config.
func (d *Daemon) handleUpdateStampingConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "POST, PATCH")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !d.isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	var patch config.StampingConfigPatch
	if !decodeJSONBody(w, r, &patch, true) {
		return
	}

	var (
		updated config.StampingConfig
		cfgPath string
	)
	d.mu.Lock()
	base := config.DefaultStampingConfig()
	if d.cfg != nil {
		base = d.cfg.EffectiveStampingConfig()
	}
	updated = config.ApplyStampingPatch(base, patch)
	if d.cfg != nil {
		d.cfg.Stamping = updated
		cfgPath = d.cfg.ConfigPath
	}
	d.mu.Unlock()

	if cfgPath != "" {
		if err := config.SaveStampingToFile(cfgPath, updated); err != nil {
			d.logger.Warn("failed to persist stamping config to file",
				slog.String("config_path", cfgPath),
				slog.Any("error", err),
			)
		}
	}

	d.writeJSONResponse(w, r, http.StatusOK, updated, "error encoding updated stamping config response")
}

// handleResetStampingConfig handles POST /api/stamping/config/reset, restoring Option 5 defaults.
func (d *Daemon) handleResetStampingConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !d.isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	defaults := config.DefaultStampingConfig()
	var cfgPath string
	d.mu.Lock()
	if d.cfg != nil {
		d.cfg.Stamping = defaults
		cfgPath = d.cfg.ConfigPath
	}
	d.mu.Unlock()

	if cfgPath != "" {
		if err := config.SaveStampingToFile(cfgPath, defaults); err != nil {
			d.logger.Warn("failed to persist reset stamping config to file",
				slog.String("config_path", cfgPath),
				slog.Any("error", err),
			)
		}
	}

	d.writeJSONResponse(w, r, http.StatusOK, defaults, "error encoding reset stamping config response")
}
