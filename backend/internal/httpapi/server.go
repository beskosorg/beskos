package httpapi

// Router builds the chi router: every route this server answers to, grouped by whether a
// session is required. The Server type and its constructor live in server_types.go.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// requestLogger replaces middleware.Logger: the session token can arrive in ?access_token=
	// (WS/media), and the default logger would write the whole URL — a 7-day token in the logs is
	// a session hijack waiting to happen. The sensitive query is redacted here before logging.
	// ClientIPFromXFFTrustedProxies(1): exactly one reverse proxy (Caddy) sits between us and the
	// public internet (architecture.md, DIVERGENCES §D7/D8), so the real client IP is the entry
	// Caddy itself appended to X-Forwarded-For -- never the leftmost one, which a client's own
	// header can freely forge. The deprecated middleware.RealIP trusted the leftmost entry.
	r.Use(middleware.RequestID, middleware.ClientIPFromXFFTrustedProxies(1), middleware.Recoverer, requestLogger)
	r.Use(s.cors) // CORS for the native app (Tauri) — no-op if there are no configured origins

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	// Readiness: healthz says the process is alive; readyz says the dependencies (Postgres) respond.
	// Load balancers/orchestrators use readyz so they don't send traffic before the database is ready.
	r.Get("/readyz", s.handleReadyz)

	r.Route("/api", func(r chi.Router) {
		// The same probes, now reachable from outside: the public proxy only forwards /api here,
		// and without this the status page would measure the static file server instead of the API.
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
		r.Get("/readyz", s.handleReadyz)

		// Public. Unified login: with a mailbox it validates against Stalwart, without one (a
		// freshly created organization) it validates against our own signup. /app no longer exists.
		//
		// /auth/signup is dev/test-only (see SetDevSignupEnabled): it creates a tenant + owner with
		// no rate limit, no signupsOpen check, and no relation to the real signup flow
		// (/public/signup, billing_routes.go) or to bootstrap-owner -- it must never be mounted in
		// a real deployment, self-hosted or hosted (audit P6).
		if s.devSignupEnabled {
			r.Post("/auth/signup", s.handleSignup)
		}
		r.Post("/auth/login", s.handleLogin)
		r.Post("/webmail/login", s.handleLogin)       // alias already used by the webmail
		r.Post("/auth/mfa/verify", s.handleMFAVerify) // 2nd step of login
		r.Get("/edition", s.handleGetEdition)         // capabilities unlocked on this instance (Community/Pro)

		// Everything behind the same session. RBAC decides what each role can see.
		r.Group(func(r chi.Router) {
			r.Use(s.requireWebmailSession)
			r.Use(s.csrfGuard)
			r.Post("/auth/logout", s.handleWebmailLogout)
			r.Post("/webmail/logout", s.handleWebmailLogout)
			s.mountMail(r) // the mailbox itself: snooze, outbox, templates, moving in
			r.Get("/me", s.handleMe)
			r.Get("/me/mail-connection", s.handleMailConnection) // IMAP/SMTP for external clients

			// MFA (second-factor enrollment). Requires an already active session.
			// Organization security policy (MFA required) — only whoever manages the org.
			r.With(s.requirePerm(rbac.PermOrgSecurity)).Get("/org/security", s.handleGetSecurityPolicy)
			r.With(s.requirePerm(rbac.PermOrgSecurity)).Put("/org/security", s.handleSetSecurityPolicyGate)

			r.Post("/mfa/totp/start", s.handleTOTPStart)
			r.Post("/mfa/totp/enable", s.handleTOTPEnable)
			r.Post("/mfa/sms/start", s.handleSMSStart)
			r.Post("/mfa/sms/enable", s.handleSMSEnable)
			r.Post("/mfa/disable", s.handleMFADisable)
			// User profile (avatar menu): data + password change (step-up).
			r.Get("/me/profile", s.handleGetMyProfile)
			r.Put("/me/profile", s.handleUpdateMyProfile)
			r.Post("/me/password", s.handleChangeMyPassword)
			r.Patch("/me/settings", s.handleUpdateSettings) // preferences (language), persisted
			r.Put("/me/status", s.handleSetMyStatus)        // user status (emoji + text)
			r.Put("/me/presence", s.handleSetPresence)      // rich presence (active/away/dnd)
			r.Get("/user-status", s.handleGetUserStatus)    // a colleague's status (by email)

			// Domains/mailboxes — gated by permission (RBAC).
			// Organization brand: any user reads it (for the app's top bar); only admin/owner writes it.
			r.Get("/branding", s.handleGetBranding)
			r.With(s.requirePerm(rbac.PermOrgBranding)).Patch("/branding", s.handleUpdateBrandingGate)

			// Organization apps: any user reads them (the rail filters); only admin/owner toggles them.
			r.Get("/apps", s.handleGetApps)
			r.With(s.requirePerm(rbac.PermOrgBranding)).Put("/apps", s.handleSetApps)

			// Organization announcements (bulletin board). Everyone reads the published ones; admin/owner manage them.
			r.Get("/announcements", s.handleListAnnouncements)
			r.With(s.requirePerm(rbac.PermAnnouncements)).Post("/announcements", s.handleCreateAnnouncement)
			r.With(s.requirePerm(rbac.PermAnnouncements)).Patch("/announcements/{id}", s.handleSetAnnouncementPublished)
			r.With(s.requirePerm(rbac.PermAnnouncements)).Delete("/announcements/{id}", s.handleDeleteAnnouncement)

			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains", s.handleListDomains)
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains/{id}", s.handleGetDomain)
			// DNS status record by record + export the zone (docs/DNS-PAGE.md). Informational: the
			// worker is still what promotes the domain.
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains/{id}/dns/check", s.handleCheckDomainDNS)
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains/{id}/dns/export", s.handleExportDomainDNS)
			r.With(s.requirePerm(rbac.PermDomainsManage)).Post("/domains", s.handleCreateDomain)
			r.With(s.requirePerm(rbac.PermDomainsDelete)).Delete("/domains/{id}", s.handleDeleteDomain)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/domains/{id}/mailboxes", s.handleCreateMailbox)
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/mailboxes", s.handleListMailboxes)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/mailboxes/{id}/password", s.handleResetMailboxPassword)
			// Mailbox lifecycle (mailbox_lifecycle.go). Deleting has its own permission because it is
			// destructive: running mailboxes and destroying them are different jobs.
			r.With(s.requirePerm(rbac.PermMailboxManage)).Put("/mailboxes/{id}/status", s.handleSetMailboxStatus)
			r.With(s.requirePerm(rbac.PermMailboxDelete)).Delete("/mailboxes/{id}", s.handleDeleteMailbox)
			r.With(s.requirePerm(rbac.PermMailboxDelete)).Post("/mailboxes/{id}/restore", s.handleRestoreMailbox)
			// Delete OR return (GDPR 28(3)(g)): during the window the data can still be taken out.
			r.With(s.requirePerm(rbac.PermMailboxDelete)).Get("/mailboxes/{id}/export", s.handleExportMailbox)

			// Email aliases (contact@company.com → someone's mailbox). Managing is a mailbox
			// operation; viewing the list only requires seeing the domains. The user's OWN addresses
			// (the composer's "From:") aren't administration — any authenticated user reads their own.
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/aliases", s.handleListAliases)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/aliases", s.handleCreateAlias)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Delete("/aliases/{id}", s.handleDeleteAlias)
			r.Get("/me/addresses", s.handleMyAddresses)

			// Users = mailbox + role (unified). Creating provisions mailbox + account together.
			r.With(s.requirePerm(rbac.PermMembersInvite)).Post("/users", s.handleCreateUser)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/users/reset-password", s.handleResetUserPassword)

			// Members and invites (RBAC).
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/members", s.handleListMembers)
			r.With(s.requirePerm(rbac.PermMembersManage)).Patch("/members/{id}/role", s.handleChangeRole)
			// Per-member permission exceptions (granular RBAC): view and save.
			r.With(s.requirePerm(rbac.PermMembersManage)).Get("/members/{id}/permissions", s.handleGetMemberPermissions)
			r.With(s.requirePerm(rbac.PermMembersManage)).Put("/members/{id}/permissions", s.handleSetMemberPermissions)
			r.With(s.requirePerm(rbac.PermMembersManage)).Delete("/members/{id}", s.handleRemoveMember)
			// The user's OWN groups (a user can be in several).
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/members/{id}/groups", s.handleUserGroups)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Put("/members/{id}/groups", s.handleSetUserGroups)
			r.With(s.requirePerm(rbac.PermMembersManage)).Get("/invites", s.handleListInvites)
			r.With(s.requirePerm(rbac.PermMembersInvite)).Post("/invites", s.handleCreateInvite)
			r.With(s.requirePerm(rbac.PermMembersManage)).Delete("/invites/{id}", s.handleRevokeInvite)

			// User groups (creating a group creates the private channel in chat).
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/groups", s.handleListGroups)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Post("/groups", s.handleCreateGroup)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Patch("/groups/{id}", s.handleUpdateGroup)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Delete("/groups/{id}", s.handleDeleteGroup)
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/groups/{id}/members", s.handleListGroupMembers)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Post("/groups/{id}/members", s.handleAddGroupMember)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Delete("/groups/{id}/members/{userId}", s.handleRemoveGroupMember)

			// Billing and usage/quotas.
			r.With(s.requirePerm(rbac.PermUsageView)).Get("/usage", s.handleUsage)
			s.mountBilling(r)

			// AI over email (content comes from the frontend, which reads via JMAP directly).
			// Agentic assistant (loop with tools). Distinct from /ai/*, which is single-step.
			r.Post("/assistant/chat", s.handleAssistantChat)
			r.Get("/assistant/conversations", s.handleAssistantConversations)
			r.Get("/assistant/conversations/{id}", s.handleAssistantConversation)
			r.Delete("/assistant/conversations/{id}", s.handleAssistantConversationDelete)
			r.Post("/assistant/confirm/{id}", s.handleAssistantConfirm)
			r.Get("/assistant/tasks", s.handleAssistantTasks)
			r.Post("/assistant/tasks/{id}/cancel", s.handleAssistantTaskCancel)
			// Fact memory (§8). Management screen: view, add by hand, delete item by item or
			// everything. It's the user's right to see and delete what the app knows about them (GDPR, §3.5).
			r.Get("/assistant/memory", s.handleAssistantMemory)
			r.Post("/assistant/memory", s.handleAssistantMemoryCreate)
			r.Delete("/assistant/memory/{id}", s.handleAssistantMemoryDelete)
			r.Post("/assistant/memory/clear", s.handleAssistantMemoryClear)
			// Budget (§9), visible audit trail (§6.4), and folder exclusion (§6.6).
			s.registerAssistantGovernance(r)
			// Agent profiles. Reading is for any user (the panel's selector needs the list);
			// creating, changing, and toggling is for the organization's administration.
			r.Get("/assistant/agents", s.handleAssistantAgents)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Post("/assistant/agents", s.handleAssistantAgentCreateGate)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Put("/assistant/agents/{id}", s.handleAssistantAgentUpdateGate)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Delete("/assistant/agents/{id}", s.handleAssistantAgentDeleteGate)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Post("/assistant/agents/{key}/enabled", s.handleAssistantAgentToggleGate)

			r.Post("/ai/summarize", s.handleAISummarize)
			r.Post("/ai/draft", s.handleAIDraft)
			r.Post("/ai/task", s.handleAIPlanTask)
			r.Post("/ai/extract-tasks", s.handleAIExtractTasks)
			r.Post("/ai/briefing", s.handleAIBriefing)
			r.Post("/ai/wiki-write", s.handleAIWikiWrite) // content for the wiki's BlockNote editor

			// Labels (per-tenant catalog).
			r.Get("/labels", s.handleListLabels)
			r.Post("/labels", s.handleCreateLabel)
			r.Delete("/labels/{id}", s.handleDeleteLabel)

			// Kanban.
			r.Get("/boards", s.handleListBoards)
			r.Get("/boards/archived", s.handleListArchivedBoards) // archived boards (restore)
			r.Post("/boards", s.handleCreateBoard)
			r.Post("/boards/{id}/archive", s.handleArchiveBoard)            // archive/restore board
			r.Get("/boards/{id}/archived-cards", s.handleListArchivedCards) // board's archived cards
			r.Get("/boards/{id}/automations", s.handleListAutomations)      // automation rules
			r.Post("/boards/{id}/automations", s.handleCreateAutomation)
			r.Delete("/automations/{id}", s.handleDeleteAutomation)
			r.Get("/boards/{id}/access", s.handleBoardAccess)
			r.Get("/boards/{id}/grants", s.handleListBoardGrants)
			r.Put("/boards/{id}/grants", s.handleSetBoardGrant)
			r.Delete("/boards/{id}/grants/{grantId}", s.handleDeleteBoardGrant)
			r.Get("/boards/{id}", s.handleGetBoard)
			r.Patch("/boards/{id}", s.handleUpdateBoard)
			r.Delete("/boards/{id}", s.handleDeleteBoard)
			r.Post("/boards/{id}/cards", s.handleCreateCard)
			r.Get("/cards/search", s.handleSearchCards) // tasks by key/number/title (chat's /task)
			r.Patch("/cards/{id}", s.handleUpdateCard)
			r.Patch("/cards/{id}/move", s.handleMoveCard)
			r.Post("/cards/{id}/archive", s.handleArchiveCard)       // archive/restore card
			r.Post("/cards/{id}/assignees", s.handleAddCardAssignee) // additional assignee
			r.Delete("/cards/{id}/assignees/{userId}", s.handleRemoveCardAssignee)
			r.Post("/cards/{id}/deps", s.handleAddCardDep) // dependency (blocked by)
			r.Delete("/cards/{id}/deps/{depId}", s.handleRemoveCardDep)
			r.Post("/columns/{id}/reorder", s.handleReorderColumn) // drag-and-drop vertical order
			r.Delete("/cards/{id}", s.handleDeleteCard)
			r.Post("/boards/{id}/columns", s.handleCreateColumn)
			r.Post("/boards/{id}/columns/reorder", s.handleReorderColumns) // columns' horizontal order
			r.Patch("/columns/{id}", s.handleUpdateColumn)
			r.Delete("/columns/{id}", s.handleDeleteColumn)

			// Calendar.
			r.Get("/calendars", s.handleListCalendars)
			r.Post("/calendars", s.handleCreateCalendar)
			r.Patch("/calendars/{id}", s.handleUpdateCalendar)
			r.Delete("/calendars/{id}", s.handleDeleteCalendar)
			r.Get("/events/free-busy", s.handleFreeBusy) // a colleague's schedule (busy/free only)
			// Booking: colleague's public scheduling page (Calendly-style).
			r.Get("/booking/page", s.handleGetBookingPage)
			r.Put("/booking/page", s.handleSaveBookingPage)
			r.Post("/timeoff", s.handleCreateTimeOff) // day off / vacation (+ auto-reply)
			r.Get("/events", s.handleListEvents)
			r.Post("/events", s.handleCreateEvent)
			r.Get("/events/export", s.handleExportEvents)           // iCalendar (.ics)
			r.Post("/events/import", s.handleImportEvents)          // iCalendar (.ics)
			r.Get("/events/{id}/invites", s.handleEventInvites)     // invitees + RSVP
			r.Get("/events/{id}/externals", s.handleEventExternals) // external invitees (email)
			r.Post("/events/{id}/rsvp", s.handleEventRSVP)          // reply to the invite
			r.Patch("/events/{id}", s.handleUpdateEvent)
			r.Delete("/events/{id}", s.handleDeleteEvent)
			r.Post("/events/{id}/duplicate", s.handleDuplicateEvent) // independent copy
			r.Post("/events/{id}/restore", s.handleRestoreEvent)     // undoes deletion (undo)
			r.Post("/events/{id}/meet", s.handleAddMeetToEvent)      // links a Meet room to an existing event

			// Tags (shared catalog: events, contacts, …).
			r.Get("/tags", s.handleListTags)
			r.Post("/tags", s.handleUpsertTag)
			r.Delete("/tags/{id}", s.handleDeleteTag)

			// Calendar AI.
			r.Post("/ai/calendar/parse", s.handleAIParseEvent)
			r.Post("/ai/calendar/suggest", s.handleAISuggestSlots)
			r.Post("/ai/calendar/agenda", s.handleAIAgenda)

			// CONTACTS: company or person, in a single list. Viewing is by group; editing is the owner's.
			r.Get("/contacts", s.handleListContacts)
			r.Post("/contacts", s.handleCreateContact)
			r.Post("/contacts/seen", s.handleSeenRecipients)   // records sent recipients as contacts
			r.Get("/contacts/export", s.handleExportContacts)  // vCard/CSV
			r.Post("/contacts/import", s.handleImportContacts) // vCard/CSV
			r.Post("/contacts/enrich", s.handleEnrichContacts) // links people↔companies by domain
			r.Get("/contacts/{id}", s.handleGetContact)
			r.Get("/contacts/{id}/vcard", s.handleContactVCard) // a contact as .vcf (attach to email)
			r.Patch("/contacts/{id}", s.handleUpdateContact)
			r.Delete("/contacts/{id}", s.handleDeleteContact)
			r.Post("/contacts/{id}/merge", s.handleMergeContacts)         // merges a duplicate into this one
			r.Post("/contacts/{id}/avatar", s.handleSetContactAvatar)     // links a photo (file_id) to the contact
			r.Post("/contacts/{id}/transfer", s.handleTransferContact)    // transfers ownership to a colleague
			r.Patch("/contacts/{id}/notes/{noteId}", s.handleEditCrmNote) // edit note (author)
			r.Get("/contacts/{id}/notes", s.handleListCrmNotes)
			r.Post("/contacts/{id}/notes", s.handleCreateCrmNote)
			r.Get("/contacts/{id}/tasks", s.handleListContactTasks)
			r.Delete("/crm-notes/{id}", s.handleDeleteCrmNote)
			// Wiki / knowledge base (N wikis per tenant; access resolved in the service via SQL `vis`).
			s.mountWiki(r)
			s.mountNotes(r)
			ee.Register(r, s.edition, ee.Deps{Queries: s.q}) // no-op until a Pro feature moves here
			// Sharing a contact group with teams AND/OR people — only the group's owner decides.
			r.Get("/contact-groups/{id}/shares", s.handleListGroupShares)
			r.Put("/contact-groups/{id}/shares", s.handleSetGroupShares)
			r.Get("/contact-share-teams", s.handleShareTeams) // teams for the picker (doesn't require admin)
			// Tenant colleagues (id/name/email): whoever does a task is someone on the team.
			r.Get("/colleagues", s.handleListColleagues)
			// User preferences (signature, per-app prefs).
			r.Get("/settings", s.handleGetSettings)
			r.Patch("/settings", s.handleUpdateSettings)
			// Devices: tokens for headless clients (sync daemon). The user creates/lists/
			// revokes their own; the raw token only comes back on creation (we only store the hash).
			r.Get("/devices", s.handleListDeviceTokens)
			r.Post("/devices", s.handleCreateDeviceToken)
			r.Delete("/devices/{id}", s.handleRevokeDeviceToken)
			// Task comments (trigger the notifications — see tasks_notify.go).
			r.Patch("/boards/{id}/prefix", s.handleUpdateBoardPrefix)
			// Personal GIF library (the GIF is a Drive file; this is just the drawer).
			r.Get("/gifs", s.handleListGifs)
			r.Post("/gifs", s.handleAddGif)
			r.Delete("/gifs/{id}", s.handleDeleteGif)
			r.Get("/cards/{id}/comments", s.handleListCardComments)
			r.Post("/cards/{id}/comments", s.handleCreateCardComment)
			r.Patch("/cards/{id}/comments/{cid}", s.handlePatchCardComment) // edit comment (author)
			r.Delete("/cards/{id}/comments/{cid}", s.handleDeleteCardComment)
			r.Get("/cards/{id}/activity", s.handleListCardActivity)
			r.Get("/cards/{id}/attachments", s.handleListCardAttachments)
			r.Post("/cards/{id}/attachments", s.handleAttachFile)
			r.Delete("/cards/{id}/attachments/{attId}", s.handleDetachFile)
			r.Get("/cards/{id}/attachments/{attId}/download", s.handleDownloadCardAttachment)
			r.Get("/cards/{id}/checklist", s.handleListChecklist)
			r.Post("/cards/{id}/checklist", s.handleCreateChecklistItem)
			r.Patch("/cards/{id}/checklist/{itemId}", s.handleToggleChecklistItem)
			r.Delete("/cards/{id}/checklist/{itemId}", s.handleDeleteChecklistItem)
			r.Patch("/contacts/{id}", s.handleUpdateContact)
			r.Delete("/contacts/{id}", s.handleDeleteContact)
			r.Get("/contact-groups", s.handleListContactGroups)
			r.Get("/contact-groups/{id}/emails", s.handleContactGroupEmails) // distribution list
			r.Post("/contact-groups", s.handleCreateContactGroup)
			r.Patch("/contact-groups/{id}", s.handleUpdateContactGroup)
			r.Delete("/contact-groups/{id}", s.handleDeleteContactGroup)

			// Storage (files over MinIO) — folders, move, share.
			r.Get("/files", s.handleListFiles)
			r.Get("/files/search", s.handleSearchFiles)      // recursive global search by name
			r.Get("/files/changes", s.handleListFileChanges) // incremental feed for the sync daemon
			r.Get("/files/trash", s.handleListTrash)         // trash (soft-deleted)
			r.Delete("/files/trash", s.handleEmptyTrash)     // empty trash (purges everything)
			r.Post("/files", s.handleUploadFile)
			r.Post("/files/batch", s.handleBatchFiles) // batch actions: trash | restore | move
			r.Get("/files/{id}/download", s.handleDownloadFile)
			r.Get("/files/{id}/thumb", s.handleThumbnail)        // JPEG thumbnail (images only)
			r.Post("/files/{id}/star", s.handleStarFile)         // favorite on/off
			r.Post("/files/{id}/copy", s.handleCopyFile)         // duplicate (reuses the blob)
			r.Patch("/files/{id}", s.handlePatchFile)            // move/rename
			r.Delete("/files/{id}", s.handleDeleteFile)          // soft delete → trash
			r.Post("/files/{id}/restore", s.handleRestoreFile)   // restore from trash
			r.Delete("/files/{id}/permanent", s.handlePurgeFile) // delete for good (irreversible)
			r.Get("/files/{id}/versions", s.handleListVersions)  // version history
			r.Get("/files/{id}/versions/{versionId}/download", s.handleDownloadVersion)
			r.Post("/files/{id}/versions/{versionId}/restore", s.handleRestoreVersion)
			r.Post("/files/{id}/share", s.handleShareFile)
			r.Get("/files/{id}/shares", s.handleListShares)
			r.Delete("/files/{id}/shares/{shareId}", s.handleDeleteShare)
			r.Get("/files/limits", s.handleFileLimits)           // per-file ceiling (env)
			r.Get("/files-shared", s.handleListSharedWithMe)     // "shared with me"
			r.Get("/files/{id}/link", s.handleGetFileLink)       // active public link token (or empty)
			r.Post("/files/{id}/link", s.handleCreateFileLink)   // creates/reuses the public link (large attachments)
			r.Delete("/files/{id}/link", s.handleRevokeFileLink) // revokes the public link
			r.Post("/files/{id}/share-recipients", s.handleShareWithRecipients)
			r.Post("/files/{id}/share-group", s.handleShareWithGroup) // shares with a team (expands into emails)
			r.Get("/my-groups", s.handleMyGroups)                     // user's teams (for sharing)
			r.Post("/folders", s.handleCreateFolder)
			r.Get("/folders/{id}/zip", s.handleZipFolder) // downloads the folder (recursive) as .zip
			r.Patch("/folders/{id}", s.handleRenameFolder)
			r.Post("/folders/{id}/move", s.handleMoveFolder) // changes the parent (with a cycle guard)
			r.Delete("/folders/{id}", s.handleDeleteFolder)

			// Office editing (OnlyOffice): create a blank file + open the editor session.
			r.Get("/docs/recent", s.handleListRecentDocuments) // the Documents app: a view over Drive
			r.Post("/files/office", s.handleCreateOfficeFile)
			r.Post("/files/{id}/edit-session", s.handleEditSession)
			r.Get("/ai/editor-key", s.handleAIEditorKey) // editor AI plugin config (proxy)

			s.routesMeet(r)

			// In-app notifications (bell).
			r.Get("/notifications", s.handleListNotifications)
			r.Post("/notifications/{id}/read", s.handleMarkNotificationRead)
			r.Post("/notifications/read-all", s.handleMarkAllNotificationsRead)
			r.Post("/notifications/read-kind", s.handleMarkNotificationsReadByKind)

			// Web Push (desktop notifications with the app closed). The browser subscribes with the
			// public VAPID key and sends the subscription; the server sends the push in notifyUser.
			r.Get("/push/vapid", s.handlePushVapid)
			r.Post("/push/subscribe", s.handlePushSubscribe)
			r.Post("/push/unsubscribe", s.handlePushUnsubscribe)

			// Chat (Slack-style).
			r.Get("/chat/channels", s.handleListChannels)
			r.Post("/chat/channels", s.handleCreateChannel)
			r.Post("/chat/channels/{id}/join", s.handleJoinChannel)
			r.Patch("/chat/channels/{id}", s.handleUpdateChannel)                      // settings (manager)
			r.Delete("/chat/channels/{id}", s.handleDeleteChannel)                     // delete channel (owner)
			r.Post("/chat/channels/{id}/members", s.handleChatAddMember)               // add member
			r.Delete("/chat/channels/{id}/members/{userId}", s.handleChatRemoveMember) // remove / leave
			r.Patch("/chat/channels/{id}/members/{userId}", s.handleChatSetMemberRole) // promote to manager
			r.Post("/chat/channels/{id}/schedule", s.handleScheduleMessage)            // schedule a message send
			r.Get("/chat/scheduled", s.handleListScheduled)                            // my scheduled messages
			r.Delete("/chat/scheduled/{id}", s.handleCancelScheduled)                  // cancels a scheduled one
			r.Post("/chat/messages/{id}/remind", s.handleRemindMessage)                // remind me of this message
			r.Get("/chat/emojis", s.handleListEmojis)                                  // workspace's custom emojis
			r.Post("/chat/emojis", s.handleCreateEmoji)                                // links :name: to a file (image)
			r.Delete("/chat/emojis/{id}", s.handleDeleteEmoji)                         // removes a custom emoji
			r.Post("/chat/dm", s.handleOpenDM)
			r.Post("/chat/group-dm", s.handleOpenGroupDM)                  // group DM (3+ people)
			r.Get("/chat/channels/archived", s.handleListArchivedChannels) // archived channels (restore)
			r.Post("/chat/channels/{id}/archive", s.handleArchiveChannel)  // archive/restore (channel owner or admin)
			r.Get("/chat/channels/{id}/webhooks", s.handleListWebhooks)    // channel integrations/bots
			r.Post("/chat/channels/{id}/webhooks", s.handleCreateWebhook)  // creates an incoming token
			r.Delete("/chat/webhooks/{id}", s.handleDeleteWebhook)         // removes an integration
			r.Get("/chat/channels/{id}/members", s.handleListChannelMembers)
			r.Get("/chat/channels/{id}/members/search", s.handleSearchChannelMembers) // paginated/search
			r.Get("/chat/channels/{id}/canvas", s.handleGetCanvas)
			r.Put("/chat/channels/{id}/canvas", s.handleSaveCanvas)
			r.Get("/chat/channels/{id}/pins", s.handleListPins)
			r.Post("/chat/messages/{id}/pin", s.handlePinMessage)
			// Bookmarks (curated files & links) — any member adds/removes.
			r.Get("/chat/channels/{id}/bookmarks", s.handleListBookmarks)
			r.Post("/chat/channels/{id}/bookmarks", s.handleAddBookmark)
			r.Delete("/chat/channels/{id}/bookmarks/{bid}", s.handleDeleteBookmark)
			r.Get("/chat/channels/{id}/bookmarks/{bid}/download", s.handleBookmarkDownload)
			r.Post("/chat/channels/{id}/notif", s.handleSetChannelNotifPref)   // mute channel
			r.Get("/chat/channels/{id}/search", s.handleSearchChannelMessages) // search within the channel
			r.Get("/chat/channels/{id}/messages", s.handleListMessages)
			r.Post("/chat/channels/{id}/messages", s.handleSendMessage)
			r.Post("/chat/channels/{id}/read", s.handleMarkChannelRead)
			r.Post("/chat/messages/{id}/unread", s.handleMarkChannelUnread) // mark unread from here on
			r.Patch("/chat/messages/{id}", s.handleEditMessage)
			r.Delete("/chat/messages/{id}", s.handleDeleteMessage)
			r.Post("/chat/messages/{id}/forward", s.handleForwardMessage) // forward to another channel
			r.Post("/chat/messages/{id}/save", s.handleSaveMessage)       // save (saved items)
			r.Delete("/chat/messages/{id}/save", s.handleUnsaveMessage)
			r.Get("/chat/saved", s.handleListSaved)
			r.Get("/chat/unfurl", s.handleUnfurl) // link preview (with SSRF guard)
			r.Get("/chat/messages/{id}/replies", s.handleThreadReplies)
			r.Get("/chat/files/{fileId}/download", s.handleChatAttachment)
			r.Get("/chat/presence", s.handleChatPresence)
			r.Get("/chat/search", s.handleChatSearch)
			r.Post("/chat/messages/{id}/react", s.handleReact)
			r.Get("/chat/ws", s.handleChatWS)
		})

		// Directory sync (headless daemon): a separate group, authenticated by a DEVICE TOKEN
		// (not a session). Minimal scope — only reading the changes feed and downloading files;
		// the token doesn't reach the rest of the API. No csrfGuard (not a cookie/browser flow).
		r.Group(func(r chi.Router) {
			r.Use(s.requireDeviceToken)
			r.Get("/sync/files/changes", s.handleListFileChanges)
			r.Get("/sync/files/{id}/download", s.handleDownloadFile)
			// Writes: PUT requires If-Match (optimistic concurrency); POST creates a new file.
			r.Put("/sync/files/{id}", s.handleSyncUpdateFile)
			r.Post("/sync/files", s.handleSyncCreateFile)
			// Folder tree (subfolder mirror): list everything + create the missing ones.
			r.Get("/sync/folders", s.handleListSyncFolders)
			r.Post("/sync/folders", s.handleCreateSyncFolder)
			// Mirrors what happens in the local folder: delete → trash (recoverable); move/rename.
			r.Post("/sync/files/{id}/trash", s.handleSyncTrashFile)
			r.Post("/sync/files/{id}/move", s.handleSyncMoveFile)
			// Delta upload: block hashes, only the changed blocks are sent.
			r.Get("/sync/files/{id}/blocks", s.handleSyncFileBlocks)
			r.Put("/sync/files/{id}/delta", s.handleSyncUpdateFileDelta)
		})

		// Accepting an invite is public (the user doesn't have an account yet).
		r.Get("/invites/{token}", s.handleGetInvite)
		r.Post("/invites/accept", s.handleAcceptInvite)

		s.mountPublicBilling(r)

		s.mountPublic(r)

		// AI proxy for the OnlyOffice plugin (OpenAI-compatible). NO cookie: the DS's iframe is
		// cross-origin and authenticates via Bearer (the user's AI token). The real DeepSeek key
		// stays on the server; the quota is the user's. OPTIONS for the CORS preflight.
		r.Options("/ai/openai/v1/chat/completions", s.handleAIChatCompletions)
		r.With(s.publicRateLimit).Post("/ai/openai/v1/chat/completions", s.handleAIChatCompletions)
		r.Options("/ai/openai/v1/models", s.handleAIModels)
		r.With(s.publicRateLimit).Get("/ai/openai/v1/models", s.handleAIModels)
		// AI plugin's server mode (aiSettings.proxy): receives the envelope and the editor's JWT.
		r.Options("/ai/proxy", s.handleAIEnvelopeProxy)
		r.With(s.publicRateLimit).Post("/ai/proxy", s.handleAIEnvelopeProxy)
	})

	// The Document Server's ai-proxy uses ONLY aiSettings.proxy's origin and appends the
	// provider's path (/v1/chat/completions). That's why the target hits the API's ROOT, not under
	// /api. Authenticates via the editor's JWT, which the DS forwards; the quota is the user's.
	r.Options("/v1/chat/completions", s.handleAIEnvelopeProxy)
	r.With(s.publicRateLimit).Post("/v1/chat/completions", s.handleAIEnvelopeProxy)
	r.With(s.publicRateLimit).Get("/v1/models", s.handleAIModelsEditor)

	// Document Server (OnlyOffice) endpoints — called server-to-server, authenticated by an HMAC
	// token in the query string (not the cookie session). They live outside /api for that reason.
	r.Get("/doc/files/{id}/download", s.handleDocDownload)
	r.Post("/doc/callback/{id}", s.handleDocCallback)

	// LiveKit webhook (server-to-server, authenticated by LiveKit's signature).
	r.Post("/meet/webhook", s.handleMeetWebhook)

	// Presentation source for the Document Server (server-to-server via filesHostURL). Lives at the
	// root because the DS reaches it directly via the internal URL, bypassing Vite/Caddy. The
	// client-facing PDF is under /api/public/meet/presentation/{id}/pdf (forwarded). Both use an HMAC token.
	r.Get("/meet/presentation-src/{token}", s.handlePresentationSource)

	// JMAP → Stalwart proxy for the native app (Tauri). Only turns on with APP_ORIGINS; on the web
	// Caddy/Vite already routes /jmap same-origin. See jmap_proxy.go.
	s.mountJMAPProxy(r)

	return r
}
