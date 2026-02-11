package handlers

import (
	"net/http"
	"strings"

	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/utils"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) renderLoginPage(w http.ResponseWriter, r *http.Request) error {
	nextPath := getSafeRedirectPath(r.URL.Query().Get("next"), "/hotels")
	if session.CurrentUser(r) != nil {
		http.Redirect(w, r, nextPath, http.StatusFound)
		return nil
	}

	return a.renderHTML(w, http.StatusOK, "login.html", map[string]any{
		"next":         nextPath,
		"errorMessage": "",
		"emailValue":   "",
	})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	email := strings.ToLower(utils.ToTrimmedString(payload["email"]))
	password := utils.ToTrimmedString(payload["password"])
	nextPath := getSafeRedirectPath(utils.ToTrimmedString(payload["next"]), "/hotels")

	sendInvalidCredentials := func() error {
		return a.renderHTML(w, http.StatusUnauthorized, "login.html", map[string]any{
			"next":         nextPath,
			"errorMessage": "Invalid credentials",
			"emailValue":   email,
		})
	}

	if email == "" || password == "" {
		return sendInvalidCredentials()
	}

	user, err := a.Store.FindUserByEmail(r.Context(), email)
	if err != nil {
		return err
	}
	if user == nil || user.PasswordHash == "" {
		return sendInvalidCredentials()
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return sendInvalidCredentials()
	}

	if err := a.Sessions.StartSession(w, r, user.ID.Hex(), user.Email, user.Role); err != nil {
		return err
	}

	http.Redirect(w, r, nextPath, http.StatusFound)
	return nil
}

func (a *App) renderRegisterPage(w http.ResponseWriter, r *http.Request) error {
	nextPath := getSafeRedirectPath(r.URL.Query().Get("next"), "/bookings")
	if session.CurrentUser(r) != nil {
		http.Redirect(w, r, nextPath, http.StatusFound)
		return nil
	}

	return a.renderHTML(w, http.StatusOK, "register.html", map[string]any{
		"next":         nextPath,
		"errorMessage": "",
		"emailValue":   "",
	})
}

func (a *App) register(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	nextPath := getSafeRedirectPath(utils.ToTrimmedString(payload["next"]), "/bookings")
	errors, userPayload := utils.ValidateRegisterPayload(payload)
	emailValue := strings.ToLower(utils.ToTrimmedString(payload["email"]))

	if len(errors) > 0 {
		return a.renderHTML(w, http.StatusBadRequest, "register.html", map[string]any{
			"next":         nextPath,
			"errorMessage": errors[0],
			"emailValue":   emailValue,
		})
	}

	existingUser, err := a.Store.FindUserByEmail(r.Context(), userPayload.Email)
	if err != nil {
		return err
	}
	if existingUser != nil {
		return a.renderHTML(w, http.StatusConflict, "register.html", map[string]any{
			"next":         nextPath,
			"errorMessage": "Email is already used",
			"emailValue":   emailValue,
		})
	}

	insertedID, err := a.Store.CreateUser(r.Context(), userPayload.Email, userPayload.Password, "user")
	if err != nil {
		if models.IsDuplicateKeyError(err, "email") {
			return a.renderHTML(w, http.StatusConflict, "register.html", map[string]any{
				"next":         nextPath,
				"errorMessage": "Email is already used",
				"emailValue":   emailValue,
			})
		}

		return a.renderHTML(w, http.StatusInternalServerError, "register.html", map[string]any{
			"next":         nextPath,
			"errorMessage": "Registration is temporarily unavailable. Please try again.",
			"emailValue":   emailValue,
		})
	}

	if err := a.Sessions.StartSession(w, r, insertedID, userPayload.Email, "user"); err != nil {
		return err
	}

	http.Redirect(w, r, nextPath, http.StatusFound)
	return nil
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	nextPath := getSafeRedirectPath(utils.ToTrimmedString(payload["next"]), "/hotels")
	a.Sessions.DestroySession(w, r)
	http.Redirect(w, r, nextPath, http.StatusFound)
	return nil
}

func (a *App) getSessionStatusAPI(w http.ResponseWriter, r *http.Request) error {
	user := session.CurrentUser(r)
	if user == nil {
		a.writeJSON(w, http.StatusUnauthorized, map[string]any{"authenticated": false})
		return nil
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
	})
	return nil
}
