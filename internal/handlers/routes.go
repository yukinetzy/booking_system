package handlers

import (
	"net/http"
	"net/url"

	"easybook/internal/middleware"

	"github.com/go-chi/chi/v5"
)

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestLogger)
	r.Use(middleware.StaticMiddleware("public"))
	r.Use(a.Sessions.Middleware)

	r.Get("/", a.withError(a.renderHomePage))
	r.Get("/about", a.withError(a.renderAboutPage))
	r.Get("/contact", a.withError(a.renderContactPage))
	r.Post("/contact", a.withError(a.handleContactForm))
	r.Get("/terms", a.withError(a.renderTermsPage))
	r.Get("/privacy", a.withError(a.renderPrivacyPage))
	r.Get("/notifications", a.withError(a.renderNotificationsPage))
	r.Get("/hotel-wait", a.withError(a.renderHotelWaitPage))
	r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		http.Redirect(w, r, "/hotels?q="+url.QueryEscape(q), http.StatusFound)
	})

	r.Get("/login", a.withError(a.renderLoginPage))
	r.Post("/login", a.withError(a.login))
	r.Get("/register", a.withError(a.renderRegisterPage))
	r.Post("/register", a.withError(a.register))
	r.Post("/logout", a.withError(a.logout))

	r.Get("/hotels", a.withError(a.renderHotelsPage))
	r.Group(func(admin chi.Router) {
		admin.Use(middleware.RequireRole("admin"))
		admin.Get("/hotels/new", a.withError(a.renderNewHotelPage))
		admin.Post("/hotels", a.withError(a.createHotelFromPage))
		admin.Get("/hotels/{id}/edit", a.withError(a.renderEditHotelPage))
		admin.Post("/hotels/{id}", a.withError(a.updateHotelFromPage))
		admin.Post("/hotels/{id}/delete", a.withError(a.deleteHotelFromPage))
	})
	r.Get("/hotels/{id}", a.withError(a.renderHotelDetailsPage))
	r.With(middleware.RequireAuth).Post("/hotels/{id}/rate", a.withError(a.rateHotelFromPage))

	r.Group(func(protected chi.Router) {
		protected.Use(middleware.RequireAuth)
		protected.Get("/bookings", a.withError(a.renderBookingsPage))
		protected.Get("/bookings/new", a.withError(a.renderNewBookingPage))
		protected.Post("/bookings", a.withError(a.createBookingFromPage))
		protected.Get("/bookings/{id}", a.withError(a.renderBookingDetailsPage))
		protected.Get("/bookings/{id}/edit", a.withError(a.renderEditBookingPage))
		protected.Post("/bookings/{id}", a.withError(a.updateBookingFromPage))
		protected.Post("/bookings/{id}/delete", a.withError(a.deleteBookingFromPage))
	})

	r.Route("/api", func(api chi.Router) {
		api.Get("/auth/session", a.withError(a.getSessionStatusAPI))
		api.Get("/hotels", a.withError(a.getHotelsAPI))
		api.Get("/hotels/{id}", a.withError(a.getHotelByIDAPI))
		api.Get("/hotels/{id}/presence/status", a.withError(a.getHotelPresenceStatusAPI))
		api.Post("/hotels/{id}/presence/heartbeat", a.withError(a.heartbeatHotelPresenceAPI))

		api.Group(func(admin chi.Router) {
			admin.Use(middleware.RequireRole("admin"))
			admin.Post("/hotels", a.withError(a.createHotelAPI))
			admin.Put("/hotels/{id}", a.withError(a.updateHotelAPI))
			admin.Delete("/hotels/{id}", a.withError(a.deleteHotelAPI))
		})
		api.With(middleware.RequireAuth).Post("/hotels/{id}/rate", a.withError(a.rateHotelAPI))

		api.Group(func(protected chi.Router) {
			protected.Use(middleware.RequireAuth)
			protected.Get("/bookings/availability", a.withError(a.getBookingAvailabilityAPI))
			protected.Get("/bookings", a.withError(a.getBookingsAPI))
			protected.Get("/bookings/{id}", a.withError(a.getBookingByIDAPI))
			protected.Post("/bookings", a.withError(a.createBookingAPI))
			protected.Put("/bookings/{id}", a.withError(a.updateBookingAPI))
			protected.Delete("/bookings/{id}", a.withError(a.deleteBookingAPI))

			protected.Post("/notifications/subscribe", a.withError(a.subscribeNotificationsAPI))
			protected.Get("/notifications", a.withError(a.getNotificationsAPI))
			protected.Post("/notifications/read-all", a.withError(a.markAllNotificationsReadAPI))
			protected.Post("/notifications/{id}/read", a.withError(a.markNotificationReadAPI))
		})
	})

	r.NotFound(a.NotFoundHandler)

	return r
}
