package httpapi

import (
	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/rbac"
)

// Wiki routes. They live here rather than in server.go for the same reason the notes ones do:
// server.go is the shape of the API and was long past the 600 lines the project allows.
func (s *Server) mountWiki(r chi.Router) {
	r.Get("/wiki/home", s.handleGetWikiHome)
	r.With(s.requirePerm(rbac.PermWikiManage)).Put("/wiki/home", s.handleSetWikiHome)
	r.Get("/wiki/spaces", s.handleListWikiSpaces)
	r.Post("/wiki/spaces", s.handleCreateWikiSpace)
	r.Get("/wiki/spaces/{id}", s.handleGetWikiSpace)
	r.Patch("/wiki/spaces/{id}", s.handleUpdateWikiSpace)
	r.Delete("/wiki/spaces/{id}", s.handleDeleteWikiSpace)
	r.Get("/wiki/spaces/{id}/grants", s.handleListWikiSpaceGrants)
	r.Put("/wiki/spaces/{id}/grants", s.handleSetWikiSpaceGrant)
	r.Delete("/wiki/spaces/{id}/grants/{grantId}", s.handleDeleteWikiSpaceGrant)
	r.Post("/wiki/spaces/{id}/public", s.handleSetWikiPublic)
	r.Get("/wiki/spaces/{id}/tree", s.handleWikiTree)
	r.Get("/wiki/spaces/{id}/templates", s.handleListWikiTemplates)
	r.Post("/wiki/spaces/{id}/pages", s.handleCreateWikiPage)
	// Notes (migration 00133): personal and meeting notes, shared per person/team/org.
	r.Get("/notes", s.handleListNotes)
	r.Post("/notes", s.handleCreateNote)
	r.Get("/notes/{id}", s.handleGetNote)
	r.Patch("/notes/{id}", s.handleUpdateNote)
	r.Delete("/notes/{id}", s.handleDeleteNote)
	r.Post("/notes/{id}/pin", s.handlePinNote)
	r.Delete("/notes/{id}/pin", s.handlePinNote)
	r.Get("/notes/{id}/grants", s.handleListNoteGrants)
	r.Post("/notes/{id}/grants", s.handleSetNoteGrant)
	r.Delete("/notes/{id}/grants/{grantId}", s.handleDeleteNoteGrant)
	r.Get("/wiki/pages/{id}", s.handleGetWikiPage)
	r.Patch("/wiki/pages/{id}", s.handleUpdateWikiPage)
	r.Delete("/wiki/pages/{id}", s.handleDeleteWikiPage)
	r.Post("/wiki/pages/{id}/move", s.handleMoveWikiPage)
	r.Post("/wiki/pages/{id}/template", s.handleSetWikiPageTemplate)
	r.Get("/wiki/pages/{id}/grants", s.handleListWikiPageGrants)
	r.Put("/wiki/pages/{id}/grants", s.handleSetWikiPageGrant)
	r.Delete("/wiki/pages/{id}/grants/{grantId}", s.handleDeleteWikiPageGrant)
	r.Get("/wiki/pages/{id}/collab/ws", s.handleWikiCollabWS)
	r.Get("/wiki/pages/{id}/versions", s.handleListWikiPageVersions)
	r.Get("/wiki/pages/{id}/versions/{version}", s.handleGetWikiPageVersion)
	r.Post("/wiki/pages/{id}/versions/{version}/restore", s.handleRestoreWikiPageVersion)
	r.Get("/wiki/pages/{id}/comments", s.handleListWikiComments)
	r.Post("/wiki/pages/{id}/comments", s.handleCreateWikiComment)
	r.Delete("/wiki/comments/{id}", s.handleDeleteWikiComment)
	r.Get("/wiki/pages/{id}/attachments", s.handleListWikiAttachments)
	r.Post("/wiki/pages/{id}/attachments", s.handleAttachWikiFile)
	r.Delete("/wiki/pages/{id}/attachments/{attId}", s.handleDetachWikiFile)
	r.Get("/wiki/pages/{id}/attachments/{attId}/download", s.handleDownloadWikiAttachment)
}
