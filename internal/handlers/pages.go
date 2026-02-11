package handlers

import (
	"fmt"
	"net/http"

	"easybook/internal/utils"
	"easybook/internal/view"
)

func (a *App) renderHomePage(w http.ResponseWriter, r *http.Request) error {
	return a.sendStaticPage(w, r, "index.html", http.StatusOK)
}

func (a *App) renderAboutPage(w http.ResponseWriter, r *http.Request) error {
	return a.sendStaticPage(w, r, "about.html", http.StatusOK)
}

func (a *App) renderTermsPage(w http.ResponseWriter, r *http.Request) error {
	return a.sendStaticPage(w, r, "terms.html", http.StatusOK)
}

func (a *App) renderPrivacyPage(w http.ResponseWriter, r *http.Request) error {
	return a.sendStaticPage(w, r, "privacy.html", http.StatusOK)
}

func (a *App) renderContactPage(w http.ResponseWriter, r *http.Request) error {
	successMessage := ""
	if r.URL.Query().Get("sent") == "1" {
		successMessage = "Thanks for reaching out. Our team will contact you shortly."
	}
	return a.renderContactTemplate(w, http.StatusOK, successMessage, "", map[string]string{})
}

func (a *App) handleContactForm(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	cleanPayload, validationErrors := utils.ValidateContactPayload(payload)
	if len(validationErrors) > 0 {
		return a.renderContactTemplate(w, http.StatusBadRequest, "", validationErrors[0], cleanPayload)
	}

	_, err = a.Store.CreateContactRequest(r.Context(), cleanPayload)
	if err != nil {
		return err
	}

	http.Redirect(w, r, "/contact?sent=1", http.StatusFound)
	return nil
}

func (a *App) renderContactTemplate(w http.ResponseWriter, statusCode int, successMessage, errorMessage string, values map[string]string) error {
	successHTML := ""
	if successMessage != "" {
		successHTML = fmt.Sprintf(`<div class="notice notice-success">%s</div>`, view.EscapeHTML(successMessage))
	}

	replacements := map[string]any{
		"successMessage": view.Safe(successHTML),
		"errorMessage":   errorMessage,
		"nameValue":      values["name"],
		"phoneValue":     values["phone"],
		"cityValue":      values["city"],
		"emailValue":     values["email"],
		"messageValue":   values["message"],
	}

	return a.renderHTML(w, statusCode, "contact.html", replacements)
}
