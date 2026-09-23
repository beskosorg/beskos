package httpapi

import "net/http"

// cors allows the native app (Tauri, origin tauri://localhost / https://tauri.localhost) and other
// configured origins to call the API. With no origins configured, it's a no-op (same-origin web
// doesn't need it). Echoes the exact origin (never "*") because we send credentials/allow the cookie.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				// `Range` is needed for the native app (Tauri, cross-origin): pdf.js requests ranges
				// on large PDFs and the browser does a preflight — without `Range` here, PDF
				// presentation didn't load on desktop (small ones, which don't use Range, worked fine).
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Locale, Range")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// Lets the client read the range/size headers on a cross-origin response (pdf.js).
			h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
		}
		next.ServeHTTP(w, r)
	})
}

// publicCORS opens an endpoint to any origin, WITHOUT credentials.
//
// The credentialed `cors` above echoes a known origin and allows cookies, which is right for the
// application API and wrong here: the mail audit is unauthenticated, returns only records that are
// already published in public DNS, and is meant to be called from the marketing site -- a different
// origin from the API. Listing that site's domains here would couple the API to the landing and
// would have to grow every time a domain is added.
//
// "*" is also the safer of the two: a browser refuses to send credentials to a wildcard origin, so
// this can never turn into a way to make an authenticated request from somebody else's page.
func publicCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
