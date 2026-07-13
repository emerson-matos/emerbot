package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	pkgauth "github.com/emerson/emerbot/packages/auth"
)

type Handler struct {
	store pkgauth.Store
	jwt   *pkgauth.JWT
}

func NewHandler(store pkgauth.Store, jwt *pkgauth.JWT) *Handler {
	return &Handler{store: store, jwt: jwt}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Name         string `json:"name"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		jsonError(w, "email and password are required", http.StatusBadRequest)
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		// Constant-time response to avoid user enumeration.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$dummyhashfordummycomparison"), []byte(req.Password)) //nolint:errcheck
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	accessToken, err := h.jwt.Sign(user.UserID, user.Email, user.Name)
	if err != nil {
		jsonError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	refreshToken := uuid.New().String()
	expiresAt := time.Now().Add(pkgauth.RefreshTokenTTL())
	if err := h.store.SaveRefreshToken(r.Context(), user.UserID, refreshToken, expiresAt); err != nil {
		jsonError(w, "failed to save refresh token", http.StatusInternalServerError)
		return
	}

	jsonOK(w, loginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
		Name:         user.Name,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	userID, err := h.store.ValidateRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		jsonError(w, "invalid or expired refresh token", http.StatusUnauthorized)
		return
	}

	user, err := h.store.GetUserByID(r.Context(), userID)
	if err != nil {
		jsonError(w, "user not found", http.StatusUnauthorized)
		return
	}

	accessToken, err := h.jwt.Sign(user.UserID, user.Email, user.Name)
	if err != nil {
		jsonError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	// Rotate refresh token.
	h.store.RevokeRefreshToken(r.Context(), req.RefreshToken) //nolint:errcheck
	newRefresh := uuid.New().String()
	expiresAt := time.Now().Add(pkgauth.RefreshTokenTTL())
	if err := h.store.SaveRefreshToken(r.Context(), user.UserID, newRefresh, expiresAt); err != nil {
		jsonError(w, "failed to save refresh token", http.StatusInternalServerError)
		return
	}

	jsonOK(w, loginResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		ExpiresIn:    3600,
		Name:         user.Name,
	})
}
