// Package api 提供 adortb-cdp 的 HTTP API 层。
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/adortb/adortb-cdp/internal/audience"
	"github.com/adortb/adortb-cdp/internal/export"
	"github.com/adortb/adortb-cdp/internal/journey"
	"github.com/adortb/adortb-cdp/internal/profile"
)

// Handler 聚合所有 API 端点依赖。
type Handler struct {
	profiles       profile.Store
	merger         *profile.Merger
	audienceStore  audience.Store
	audienceBuilder *audience.Builder
	journeyStore   journey.Store
	orchestrator   *journey.Orchestrator
	exporter       *export.Exporter
	logger         *slog.Logger
}

// NewHandler 依赖注入构造。
func NewHandler(
	profiles profile.Store,
	merger *profile.Merger,
	audienceStore audience.Store,
	audienceBuilder *audience.Builder,
	journeyStore journey.Store,
	orchestrator *journey.Orchestrator,
	exporter *export.Exporter,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		profiles:        profiles,
		merger:          merger,
		audienceStore:   audienceStore,
		audienceBuilder: audienceBuilder,
		journeyStore:    journeyStore,
		orchestrator:    orchestrator,
		exporter:        exporter,
		logger:          logger,
	}
}

// --- 通用响应帮助 ---

type apiResp struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiResp{Error: msg})
}

func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

// --- Profile 端点 ---

func (h *Handler) CreateOrUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req profile.UpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.CanonicalID == "" && len(req.ExternalIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "canonical_id or external_ids required")
		return
	}
	if req.CanonicalID == "" {
		req.CanonicalID = profile.GenerateCanonicalID(req.ExternalIDs)
	}

	p, err := h.profiles.Upsert(r.Context(), req)
	if err != nil {
		h.logger.Error("upsert profile", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Data: p})
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	canonicalID := r.PathValue("canonical_id")
	if canonicalID == "" {
		writeErr(w, http.StatusBadRequest, "canonical_id required")
		return
	}
	p, err := h.profiles.GetByCanonicalID(r.Context(), canonicalID)
	if errors.Is(err, profile.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 附加最近事件
	events, _ := h.profiles.ListEvents(r.Context(), canonicalID, 20, time.Now().UTC().Add(time.Hour))
	writeJSON(w, http.StatusOK, apiResp{Data: map[string]any{
		"profile": p,
		"events":  events,
	}})
}

func (h *Handler) WriteEvent(w http.ResponseWriter, r *http.Request) {
	canonicalID := r.PathValue("canonical_id")
	var ev profile.Event
	if err := decodeJSON(r, &ev); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	ev.CanonicalID = canonicalID
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if err := h.profiles.WriteEvent(r.Context(), &ev); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, apiResp{Data: map[string]any{"ok": true}})
}

func (h *Handler) IdentifyProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrimaryID   string `json:"primary_id"`
		SecondaryID string `json:"secondary_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.PrimaryID == "" || req.SecondaryID == "" {
		writeErr(w, http.StatusBadRequest, "primary_id and secondary_id required")
		return
	}

	result, err := h.merger.Merge(r.Context(), req.PrimaryID, req.SecondaryID)
	if err != nil {
		if errors.Is(err, profile.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "profile not found")
			return
		}
		h.logger.Error("merge profiles", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Data: result})
}

// --- Audience 端点 ---

func (h *Handler) CreateAudience(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Conditions  json.RawMessage `json:"conditions"`
		Type        string          `json:"type"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	cond, err := audience.ParseCondition(req.Conditions)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid conditions: "+err.Error())
		return
	}
	t := req.Type
	if t == "" {
		t = "dynamic"
	}
	aud, err := h.audienceStore.Create(r.Context(), &audience.Audience{
		Name:        req.Name,
		Description: req.Description,
		Conditions:  cond,
		Type:        t,
		Status:      "active",
	})
	if err != nil {
		h.logger.Error("create audience", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, apiResp{Data: aud})
}

func (h *Handler) GetAudienceMembers(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid audience id")
		return
	}
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	members, nextCursor, err := h.audienceStore.ListMembers(r.Context(), id, limit, cursor)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Data: map[string]any{
		"members":     members,
		"next_cursor": nextCursor,
	}})
}

func (h *Handler) ExportAudience(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid audience id")
		return
	}
	var req struct {
		Destination string `json:"destination"`
		Endpoint    string `json:"endpoint"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	result, err := h.exporter.Export(r.Context(), export.ExportRequest{
		AudienceID:  id,
		Destination: export.DestinationType(req.Destination),
		Endpoint:    req.Endpoint,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Data: result})
}

// --- Journey 端点 ---

func (h *Handler) CreateJourney(w http.ResponseWriter, r *http.Request) {
	var j journey.Journey
	if err := decodeJSON(r, &j); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if j.Status == "" {
		j.Status = "draft"
	}
	created, err := h.journeyStore.CreateJourney(r.Context(), &j)
	if err != nil {
		h.logger.Error("create journey", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, apiResp{Data: created})
}

func (h *Handler) ActivateJourney(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid journey id")
		return
	}
	if err := h.journeyStore.UpdateJourneyStatus(r.Context(), id, "active"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "journey not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Data: map[string]any{"status": "active"}})
}

func (h *Handler) GetJourneyInstance(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	journeyID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid journey id")
		return
	}
	canonicalID := r.PathValue("canonical_id")
	inst, err := h.journeyStore.GetInstance(r.Context(), journeyID, canonicalID)
	if errors.Is(err, journey.ErrInstanceNotFound) {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Data: inst})
}
