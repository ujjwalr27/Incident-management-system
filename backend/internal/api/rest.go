package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zeotap/ims/internal/auth"
	"github.com/zeotap/ims/internal/models"
	mongostore "github.com/zeotap/ims/internal/store/mongo"
	pgstore "github.com/zeotap/ims/internal/store/postgres"
	redisstore "github.com/zeotap/ims/internal/store/redis"
	"github.com/zeotap/ims/internal/workflow"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	pg     *pgstore.Store
	mg     *mongostore.Store
	rds    *redisstore.Store
	issuer *auth.Issuer
}

func New(pg *pgstore.Store, mg *mongostore.Store, rds *redisstore.Store, issuer *auth.Issuer) *Handler {
	return &Handler{pg: pg, mg: mg, rds: rds, issuer: issuer}
}

// --- Auth ---

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := h.pg.GetUserByEmail(r.Context(), req.Email)
	if err != nil || user == nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	pair, err := h.issuer.Issue(user.ID, user.Role)
	if err != nil {
		jsonError(w, "token generation failed", http.StatusInternalServerError)
		return
	}

	// Set httpOnly cookie for browser clients (SSE).
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    pair.AccessToken,
		HttpOnly: true,
		Path:     "/",
		MaxAge:   int((15 * time.Minute).Seconds()),
		SameSite: http.SameSiteLaxMode,
	})

	jsonOK(w, pair)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		jsonError(w, "refresh_token is required", http.StatusBadRequest)
		return
	}
	claims, err := h.issuer.VerifyRefresh(req.RefreshToken)
	if err != nil {
		jsonError(w, "invalid or expired refresh token", http.StatusUnauthorized)
		return
	}
	uid, _ := uuid.Parse(claims.UserID)
	pair, err := h.issuer.Issue(uid, claims.Role)
	if err != nil {
		jsonError(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    pair.AccessToken,
		HttpOnly: true,
		Path:     "/",
		MaxAge:   int((15 * time.Minute).Seconds()),
		SameSite: http.SameSiteLaxMode,
	})
	jsonOK(w, pair)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserIDFromCtx(r.Context())
	id, _ := uuid.Parse(uid)
	user, err := h.pg.GetUserByID(r.Context(), id)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	jsonOK(w, user)
}

// --- Incidents ---

func (h *Handler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100)
	offset := queryInt(r, "offset", 0)

	// Hot path: serve from Redis (active + closed zsets).
	if items, err := h.rds.GetAllIncidents(r.Context(), limit); err == nil && len(items) > 0 {
		jsonOK(w, items)
		return
	}

	// Cold cache: hit Postgres and reheat Redis.
	items, err := h.pg.ListWorkItems(r.Context(), limit, offset)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []*models.WorkItem{}
	}
	// Reheat cache in the background using a detached context — r.Context() is
	// cancelled as soon as the response is written, which would abort the upserts.
	go func() {
		ctx := context.Background()
		for _, wi := range items {
			_ = h.rds.UpsertIncident(ctx, wi)
		}
	}()
	jsonOK(w, items)
}

func (h *Handler) GetIncident(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, "invalid incident id", http.StatusBadRequest)
		return
	}

	wi, err := h.pg.GetWorkItem(r.Context(), id)
	if err != nil || wi == nil {
		jsonError(w, "incident not found", http.StatusNotFound)
		return
	}

	rca, _ := h.pg.GetRCA(r.Context(), id)
	if rca != nil {
		mttr := workflow.MTTR(wi, rca)
		wi.MTTR = &mttr
	}

	transitions, _ := h.pg.GetTransitions(r.Context(), id)

	jsonOK(w, map[string]interface{}{
		"incident":    wi,
		"rca":         rca,
		"transitions": transitions,
	})
}

func (h *Handler) GetIncidentSignals(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, "invalid incident id", http.StatusBadRequest)
		return
	}
	limit := int64(queryInt(r, "limit", 100))
	skip := int64(queryInt(r, "offset", 0))

	signals, err := h.mg.GetSignalsByWorkItem(r.Context(), id.String(), limit, skip)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, signals)
}

func (h *Handler) TransitionIncident(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, "invalid incident id", http.StatusBadRequest)
		return
	}

	var req models.TransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	wi, err := h.pg.GetWorkItem(r.Context(), id)
	if err != nil || wi == nil {
		jsonError(w, "incident not found", http.StatusNotFound)
		return
	}

	// Validate transition.
	if err := workflow.CanTransition(wi.Status, req.ToStatus); err != nil {
		var invErr *workflow.ErrInvalidTransition
		if errors.As(err, &invErr) {
			jsonError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Extra guard: CLOSED requires complete RCA.
	if req.ToStatus == models.StatusClosed {
		rca, _ := h.pg.GetRCA(r.Context(), id)
		if err := workflow.ValidateClose(rca); err != nil {
			jsonError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	uid := auth.UserIDFromCtx(r.Context())
	userUUID, _ := uuid.Parse(uid)
	if err := h.pg.TransitionStatus(r.Context(), id, wi.Status, req.ToStatus, &userUUID, req.Notes); err != nil {
		jsonError(w, "failed to transition status", http.StatusInternalServerError)
		return
	}

	// Refresh cache.
	wi.Status = req.ToStatus
	_ = h.rds.UpsertIncident(r.Context(), wi)
	_ = h.rds.Publish(r.Context(), &models.SSEEvent{Type: "incident.transitioned", Payload: wi})

	jsonOK(w, map[string]string{"status": string(req.ToStatus)})
}

// --- RCA ---

func (h *Handler) SubmitRCA(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, "invalid incident id", http.StatusBadRequest)
		return
	}

	var req models.RCARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build and validate RCA.
	uid := auth.UserIDFromCtx(r.Context())
	userUUID, _ := uuid.Parse(uid)
	rca := &models.RCA{
		ID:              uuid.New(),
		WorkItemID:      id,
		Category:        req.Category,
		FixApplied:      req.FixApplied,
		PreventionSteps: req.PreventionSteps,
		IncidentStart:   req.IncidentStart,
		IncidentEnd:     req.IncidentEnd,
		SubmittedBy:     &userUUID,
	}

	if err := workflow.ValidateClose(rca); err != nil {
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if err := h.pg.CreateRCA(r.Context(), rca); err != nil {
		jsonError(w, "failed to save RCA", http.StatusInternalServerError)
		return
	}

	_ = h.rds.Publish(r.Context(), &models.SSEEvent{Type: "rca.submitted", Payload: map[string]string{"work_item_id": id.String()}})

	jsonOK(w, rca)
}

func (h *Handler) GetRCA(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, "invalid incident id", http.StatusBadRequest)
		return
	}
	rca, err := h.pg.GetRCA(r.Context(), id)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	if rca == nil {
		jsonError(w, "RCA not found", http.StatusNotFound)
		return
	}
	jsonOK(w, rca)
}

// --- Health ---

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := map[string]string{}
	ok := true

	if err := h.pg.Ping(ctx); err != nil {
		status["postgres"] = "down: " + err.Error()
		ok = false
	} else {
		status["postgres"] = "ok"
	}

	if err := h.mg.Ping(ctx); err != nil {
		status["mongo"] = "down: " + err.Error()
		ok = false
	} else {
		status["mongo"] = "ok"
	}

	if err := h.rds.Ping(ctx); err != nil {
		status["redis"] = "down: " + err.Error()
		ok = false
	} else {
		status["redis"] = "ok"
	}

	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(status)
}

// --- helpers ---

func parseID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "id"))
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
