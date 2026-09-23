-- Control plane queries. sqlc generates typed code from here.
-- RULE: every query touching customer data filters by tenant_id (brief §5/§10).

-- name: CreateTenant :one
INSERT INTO tenants (name, plan) VALUES ($1, $2) RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id = $1;

-- name: CreateAccount :one
-- User with a password and NO mailbox (signup/invite: the tenant does not have a domain yet).
INSERT INTO mail_users (tenant_id, email, password_hash, role)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetUserByID :one
SELECT * FROM mail_users WHERE id = $1;

-- name: UpdateUserLanguage :one
UPDATE mail_users SET language = $2 WHERE id = $1 RETURNING *;

-- name: UpdateUserPasswordByEmail :exec
UPDATE mail_users SET password_hash = $3 WHERE email = $1 AND tenant_id = $2;

-- name: CreateSession :one
INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetSession :one
SELECT sqlc.embed(sessions), sqlc.embed(mail_users)
FROM sessions
JOIN mail_users ON mail_users.id = sessions.user_id
WHERE sessions.token = $1 AND sessions.expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = $1;

-- name: CreateDomain :one
INSERT INTO domains (tenant_id, domain, provision_state)
VALUES ($1, $2, 'pending') RETURNING *;

-- name: ListDomainsByTenant :many
SELECT * FROM domains WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: CountMailUsersOnDomain :one
-- Users/mailboxes whose email belongs to the domain (no FK; the link is the email suffix).
SELECT count(*) FROM mail_users WHERE tenant_id = @tenant_id AND email LIKE '%@' || @domain;

-- name: DeleteDomainRow :exec
DELETE FROM domains WHERE id = @id AND tenant_id = @tenant_id;

-- name: SetTenantMFAPolicy :exec
-- Organization's mandatory MFA policy: on/off + required method (migration 00110).
UPDATE tenants SET mfa_required = @mfa_required, mfa_method = @mfa_method WHERE id = @id;

-- name: DeleteTenantWebmailSessions :exec
-- Kills ALL of the tenant's sessions except the current one (when turning on the MFA policy:
-- everyone is forced to sign in again and lands on the enrollment gate).
DELETE FROM webmail_sessions s USING mail_users u
WHERE s.mail_user_id = u.id AND u.tenant_id = @tenant_id AND s.token <> @keep_token;

-- name: UpdateMyProfile :exec
UPDATE mail_users SET name = @name, job_title = @job_title, phone = @phone, avatar = @avatar
WHERE id = @id;

-- name: UpdateUserPassword :exec
UPDATE mail_users SET password_hash = @password_hash WHERE id = @id;

-- name: DeleteUserWebmailSessionsExcept :exec
-- Password change: drops the user's OTHER sessions (the current one stays).
DELETE FROM webmail_sessions WHERE mail_user_id = @mail_user_id AND token <> @keep_token;

-- name: GetDomainByName :one
-- A domain can only exist once on the platform (it is someone's mailbox).
SELECT * FROM domains WHERE domain = $1;

-- name: DeleteTenant :exec
-- Only self-service registration uses this, to undo a signup that failed halfway through (the
-- rest cascades). Never exposed on a route: deleting a live organization is a manager operation.
DELETE FROM tenants WHERE id = $1;

-- name: GetDomainByID :one
SELECT * FROM domains WHERE id = $1;

-- name: UpdateDomainProvision :one
UPDATE domains
SET provision_state = $2, stalwart_id = $3, dkim_status = $4, dns_status = $5, dns_zone = $6
WHERE id = $1 RETURNING *;

-- All domains, for maintenance tasks that have to go through each one (e.g., ensuring the SES
-- tenant for domains provisioned before isolation existed).
-- name: ListAllDomains :many
SELECT * FROM domains ORDER BY created_at;

-- name: ListDomainsInState :many
SELECT * FROM domains WHERE provision_state = $1 ORDER BY created_at ASC LIMIT $2;

-- name: CreateMailUser :one
INSERT INTO mail_users (tenant_id, email, name, role, stalwart_account_id, quota_bytes, password_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: ListMailUsersByTenant :many
SELECT * FROM mail_users WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: GetMailUserByEmail :one
SELECT * FROM mail_users WHERE email = $1;

-- name: GetMailUserInTenant :one
SELECT * FROM mail_users WHERE id = $1 AND tenant_id = $2;

-- name: InsertAudit :exec
INSERT INTO audit_log (tenant_id, actor, action, target) VALUES ($1, $2, $3, $4);

-- ===== Webmail session (end user on our API) =====

-- name: CreateWebmailSession :one
INSERT INTO webmail_sessions (token, mail_user_id, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetWebmailSession :one
SELECT sqlc.embed(mail_users) FROM webmail_sessions
JOIN mail_users ON mail_users.id = webmail_sessions.mail_user_id
WHERE webmail_sessions.token = $1 AND webmail_sessions.expires_at > now();

-- name: DeleteWebmailSession :exec
DELETE FROM webmail_sessions WHERE token = $1;

-- ===== Labels =====

-- name: ListLabels :many
SELECT * FROM labels WHERE tenant_id = $1 ORDER BY name;

-- name: CreateLabel :one
INSERT INTO labels (tenant_id, name, color) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateLabel :one
UPDATE labels SET name = $3, color = $4 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteLabel :exec
DELETE FROM labels WHERE id = $1 AND tenant_id = $2;

-- ===== Kanban =====

-- name: ListEventsRange :many
SELECT * FROM events
WHERE owner_user_id = $1 AND starts_at < $3 AND ends_at > $2 AND deleted_at IS NULL
ORDER BY starts_at;

-- name: CreateEvent :one
INSERT INTO events (tenant_id, owner_user_id, title, description, location, color, starts_at, ends_at, source_email_id, tags, meeting_room, remind_minutes, all_day, recur, recur_until, calendar_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING *;

-- name: UpdateEvent :one
-- reminded = false: editing re-arms the reminder (e.g., time changed, notify again).
UPDATE events SET title = $3, description = $4, location = $5, color = $6, starts_at = $7, ends_at = $8, tags = $9, remind_minutes = $11, all_day = $12, recur = $13, recur_until = $14, calendar_id = $15, reminded = false
WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $10 RETURNING *;

-- name: ListRecurringByOwner :many
-- Owner's recurring masters (recur <> ''); the backend expands the occurrences within the requested range.
SELECT * FROM events WHERE owner_user_id = $1 AND recur <> '' AND deleted_at IS NULL ORDER BY starts_at;

-- name: EventsToRemind :many
-- Events whose reminder should fire now: start within the lead time (remind_minutes), still in
-- the future, with the reminder on and not yet notified. Brings along the owner, to notify them.
SELECT id, owner_user_id, tenant_id, title, starts_at FROM events
WHERE remind_minutes > 0 AND NOT reminded AND owner_user_id IS NOT NULL AND deleted_at IS NULL
  AND starts_at > now() AND starts_at - make_interval(mins => remind_minutes) <= now()
ORDER BY starts_at LIMIT 200;

-- name: MarkEventReminded :exec
UPDATE events SET reminded = true WHERE id = $1;

-- name: DeleteEvent :exec
-- Soft-delete: sets deleted_at (can be undone via RestoreEvent). Owner only, within their tenant.
UPDATE events SET deleted_at = now() WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3 AND deleted_at IS NULL;

-- name: RestoreEvent :one
-- Undoes the deletion within the undo window. Returns the restored event (or nothing if not found).
UPDATE events SET deleted_at = NULL WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3 RETURNING *;

-- ===== RBAC: invites and members =====

-- name: CreateInvite :one
INSERT INTO invites (tenant_id, email, role, token, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetValidInvite :one
SELECT * FROM invites WHERE token = $1 AND accepted_at IS NULL AND expires_at > now();

-- name: AcceptInvite :exec
UPDATE invites SET accepted_at = now() WHERE token = $1;

-- name: ConsumeInvite :one
-- Marks the invite as accepted ATOMICALLY and returns it only if it was still valid (single use,
-- no race): two concurrent acceptances with the same token — only one gets the row.
UPDATE invites SET accepted_at = now()
WHERE token = $1 AND accepted_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListPendingInvites :many
SELECT * FROM invites WHERE tenant_id = $1 AND accepted_at IS NULL AND expires_at > now() ORDER BY created_at DESC;

-- name: RevokeInvite :exec
DELETE FROM invites WHERE id = $1 AND tenant_id = $2;

-- name: ListMembers :many
SELECT id, email, name, role, created_at, (stalwart_account_id IS NOT NULL) AS has_mailbox
FROM mail_users WHERE tenant_id = $1 ORDER BY created_at;

-- name: UpdateUserRole :one
UPDATE mail_users SET role = $3 WHERE id = $1 AND tenant_id = $2 RETURNING id, email, role, created_at;

-- name: DeleteUserInTenant :exec
DELETE FROM mail_users WHERE id = $1 AND tenant_id = $2;

-- name: CountOwners :one
SELECT count(*) FROM mail_users WHERE tenant_id = $1 AND role = 'owner';

-- ===== MFA =====

-- name: SetTOTPSecret :exec
UPDATE mail_users SET totp_secret = $2 WHERE id = $1;

-- name: EnableTOTP :exec
UPDATE mail_users SET mfa_totp_enabled = true WHERE id = $1;

-- name: SetTOTPLastStep :exec
-- Records the accepted time step (anti-replay): only moves forward, never back.
UPDATE mail_users SET last_totp_step = $2 WHERE id = $1 AND $2 > last_totp_step;

-- name: SetJmapCred :exec
-- Stores the encrypted JMAP (Basic) credential, for the proxy to inject server-to-server (brokering H11).
UPDATE mail_users SET jmap_cred = $2 WHERE id = $1;

-- name: SetPhone :exec
UPDATE mail_users SET phone = $2 WHERE id = $1;

-- name: SetPresence :exec
UPDATE mail_users SET presence = $2 WHERE id = $1;

-- name: EnableSMS :exec
UPDATE mail_users SET mfa_sms_enabled = true WHERE id = $1;

-- name: DisableMFA :exec
UPDATE mail_users SET mfa_totp_enabled = false, mfa_sms_enabled = false, totp_secret = '', phone = '' WHERE id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: ListActiveRecoveryCodes :many
SELECT id, code_hash FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: UseRecoveryCode :exec
UPDATE recovery_codes SET used_at = now() WHERE id = $1;

-- name: CreateMFAChallenge :exec
INSERT INTO mfa_challenges (token, user_id, sms_code_hash, expires_at) VALUES ($1, $2, $3, $4);

-- name: GetMFAChallenge :one
SELECT * FROM mfa_challenges WHERE token = $1 AND expires_at > now();

-- name: IncrMFAChallengeAttempts :one
UPDATE mfa_challenges SET attempts = attempts + 1 WHERE token = $1 RETURNING attempts;

-- name: DeleteMFAChallenge :exec
DELETE FROM mfa_challenges WHERE token = $1;

-- ===== Storage (MinIO) =====

-- name: CreateStorageFile :one
INSERT INTO storage_files (tenant_id, owner_user_id, name, blob_key, hash, size, version, folder_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListStorageFilesByTenant :many
SELECT * FROM storage_files WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: ListFilesInFolder :many
SELECT * FROM storage_files WHERE owner_user_id = $1 AND folder_id IS NOT DISTINCT FROM $2 AND deleted_at IS NULL ORDER BY name;

-- name: ListTrashedFiles :many
-- Owner's trash: only the soft-deleted ones, most recent first.
SELECT * FROM storage_files WHERE owner_user_id = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC;

-- name: CreateDeviceToken :one
INSERT INTO device_tokens (tenant_id, mail_user_id, name, token_hash, token_prefix, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, name, token_prefix, scope, expires_at, created_at;

-- name: GetDeviceTokenUser :one
-- Authenticates the daemon: matches the token hash, skips revoked/expired tokens, and returns
-- the mail_user (to inject into the request context, the same way the webmail session does) plus the token id (to update its last-used time).
SELECT sqlc.embed(mail_users), device_tokens.id AS token_id
FROM device_tokens
JOIN mail_users ON mail_users.id = device_tokens.mail_user_id
WHERE device_tokens.token_hash = $1
  AND device_tokens.revoked_at IS NULL
  AND (device_tokens.expires_at IS NULL OR device_tokens.expires_at > now());

-- name: TouchDeviceToken :exec
UPDATE device_tokens SET last_used_at = now() WHERE id = $1;

-- name: ListDeviceTokens :many
-- The user's own devices (the Settings UI lists/revokes them). Never exposes the hash.
SELECT id, name, token_prefix, scope, last_used_at, expires_at, revoked_at, created_at
FROM device_tokens WHERE mail_user_id = $1 ORDER BY created_at DESC;

-- name: RevokeDeviceToken :exec
-- Revokes a device belonging to the user themselves (owner-scoped). Idempotent.
UPDATE device_tokens SET revoked_at = now()
WHERE id = $1 AND mail_user_id = $2 AND revoked_at IS NULL;

-- name: ListFileChanges :many
-- Incremental feed for the sync client (daemon). Keyset on (updated_at, id): the cursor is opaque
-- to the client and never skips or repeats rows at the page boundary. Includes trashed rows
-- (deleted_at != NULL) as tombstones — the client removes them locally. `hash` acts as the
-- content's ETag.
-- Includes MY files and the ones SHARED with me (principal = my email), in a single keyset — so
-- the cursor stays a single one. `owned` tells the client whether it can write: a shared file is
-- read-only in sync (it goes to a reserved folder and does not upload changes).
SELECT f.id, f.folder_id, f.name, f.hash, f.size, f.version, f.deleted_at, f.updated_at,
       (f.owner_user_id = $2) AS owned
FROM storage_files f
WHERE f.tenant_id = $1
  AND (f.owner_user_id = $2 OR EXISTS (
        SELECT 1 FROM storage_shares sh
        WHERE sh.file_id = f.id AND sh.tenant_id = $1 AND sh.principal = $3))
  AND (f.updated_at > $4 OR (f.updated_at = $4 AND f.id > $5))
ORDER BY f.updated_at, f.id
LIMIT $6;

-- name: TrashFile :one
-- Soft delete: sends to the trash (does not delete the blob). now() marks when it happened.
UPDATE storage_files SET deleted_at = now() WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: RestoreFile :one
-- Restores from the trash: becomes active again.
UPDATE storage_files SET deleted_at = NULL WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: GetStorageFile :one
SELECT * FROM storage_files WHERE id = $1 AND owner_user_id = $2;

-- name: SetFileStarred :one
-- Marks/unmarks as favorite. Owner only (multi-tenant via owner_user_id).
UPDATE storage_files SET starred = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: SearchStorageFiles :many
-- Global (recursive) search: matches the name in ALL of the owner's folders, not just the
-- current one. LIMIT as a cap. Ignores the trash (deleted_at IS NULL).
SELECT * FROM storage_files WHERE owner_user_id = $1 AND name ILIKE $2 AND deleted_at IS NULL ORDER BY name LIMIT 200;

-- name: GetStorageFileInTenant :one
SELECT * FROM storage_files WHERE id = $1 AND tenant_id = $2;

-- name: UpdateStorageFileContent :one
UPDATE storage_files SET blob_key = $3, hash = $4, size = $5, version = version + 1
WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteStorageFile :exec
DELETE FROM storage_files WHERE id = $1 AND owner_user_id = $2;

-- name: MoveStorageFile :one
UPDATE storage_files SET folder_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: RenameStorageFile :one
UPDATE storage_files SET name = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: MoveRenameStorageFile :one
-- Moves AND renames in a single UPDATE (the sync daemon treats a local move/rename as one operation).
UPDATE storage_files SET folder_id = $3, name = $4 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- ===== Folders =====

-- name: ListFolders :many
SELECT * FROM storage_folders WHERE owner_user_id = $1 AND parent_id IS NOT DISTINCT FROM $2 ORDER BY name;

-- name: GetFolder :one
SELECT * FROM storage_folders WHERE id = $1 AND owner_user_id = $2;

-- name: ListAllFoldersByOwner :many
-- All of the owner's folders (any level) — the backend builds the tree in memory (e.g., recursive zip).
SELECT * FROM storage_folders WHERE owner_user_id = $1;

-- name: SearchStorageFolders :many
-- Folders whose name matches the search, at any level (so global search also finds folders).
SELECT * FROM storage_folders WHERE owner_user_id = $1 AND name ILIKE $2 ORDER BY name LIMIT 100;

-- name: CreateFolder :one
INSERT INTO storage_folders (tenant_id, owner_user_id, parent_id, name) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: RenameFolder :one
UPDATE storage_folders SET name = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: MoveFolder :one
-- Moves the folder to another parent (null = root). The handler blocks cycles (moving into itself/a descendant).
UPDATE storage_folders SET parent_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteFolder :exec
DELETE FROM storage_folders WHERE id = $1 AND owner_user_id = $2;

-- name: DeleteShare :exec
DELETE FROM storage_shares WHERE id = $1 AND tenant_id = $2;

-- name: CountBlobRefs :one
SELECT count(*) FROM storage_files WHERE tenant_id = $1 AND blob_key = $2;

-- name: CountBlobRefsAll :one
-- Blob in use by ANY record: current file OR historical version. The purge only removes the blob
-- from MinIO when this is 0 (otherwise it would delete content still referenced by another version/deduplicated file).
SELECT (SELECT count(*) FROM storage_files WHERE storage_files.tenant_id = $1 AND storage_files.blob_key = $2)
     + (SELECT count(*) FROM file_versions WHERE file_versions.tenant_id = $1 AND file_versions.blob_key = $2) AS total;

-- name: SnapshotFileVersion :exec
-- Snapshots a version into the history (the current state, before overwriting).
INSERT INTO file_versions (file_id, tenant_id, blob_key, hash, size, version, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListFileVersions :many
SELECT v.*, COALESCE(u.name, NULLIF(u.email, ''), '') AS author
FROM file_versions v LEFT JOIN mail_users u ON u.id = v.created_by
WHERE v.file_id = $1 AND v.tenant_id = $2 ORDER BY v.version DESC;

-- name: GetFileVersion :one
SELECT * FROM file_versions WHERE id = $1 AND file_id = $2 AND tenant_id = $3;

-- name: ListVersionBlobs :many
SELECT DISTINCT blob_key FROM file_versions WHERE file_id = $1 AND tenant_id = $2;

-- name: MaxVersionByName :one
SELECT COALESCE(max(version), 0)::int FROM storage_files WHERE tenant_id = $1 AND name = $2;

-- name: CreateShare :one
-- Upsert: sharing again with the same person CHANGES the permission instead of creating another
-- row — this is how "reader" is swapped for "editor" without deleting and redoing the share.
INSERT INTO storage_shares (file_id, tenant_id, principal, level) VALUES ($1, $2, $3, $4)
ON CONFLICT (file_id, principal) DO UPDATE SET level = EXCLUDED.level
RETURNING *;

-- name: ListSharesByFile :many
SELECT * FROM storage_shares WHERE file_id = $1 AND tenant_id = $2;

-- name: SharedFileIDs :many
SELECT DISTINCT file_id FROM storage_shares WHERE tenant_id = $1;

-- ===== Contacts (N:N groups + tags) =====

-- VISIBILITY RULE (repeated in every contact read):
--   tenant  → everyone in the organization sees it
--   private → owner only
--   groups  → the owner + anyone in a group the contact was shared with
-- ALWAYS read through here; never by owner_user_id alone (it would hide what is shared) nor
-- by tenant_id alone (it would leak what is private).

-- name: ListContacts :many
-- VISIBILITY BY GROUP: I see the contact if I am the owner, or if I am on a TEAM that one of its
-- GROUPS was shared with. A person inherits from the company: seeing Acme means seeing who works
-- there — otherwise the list would show the company and hide its people (the "of —" bug coming back).
WITH vis AS (
    SELECT c.id FROM contacts c
    WHERE c.tenant_id = $2 AND (
        c.owner_user_id = $1
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_shares s ON s.group_id = m.group_id
            JOIN user_group_members ugm ON ugm.group_id = s.user_group_id
            WHERE m.contact_id = c.id AND ugm.user_id = $1)
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_user_shares us ON us.group_id = m.group_id
            WHERE m.contact_id = c.id AND us.user_id = $1)
    )
)
SELECT c.*, co.name AS company_name
FROM contacts c
LEFT JOIN contacts co ON co.id = c.company_id
WHERE c.tenant_id = $2
  AND (c.id IN (SELECT id FROM vis) OR c.company_id IN (SELECT id FROM vis))
ORDER BY c.name;

-- name: GetContact :one
WITH vis AS (
    SELECT c.id FROM contacts c
    WHERE c.tenant_id = $3 AND (
        c.owner_user_id = $2
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_shares s ON s.group_id = m.group_id
            JOIN user_group_members ugm ON ugm.group_id = s.user_group_id
            WHERE m.contact_id = c.id AND ugm.user_id = $2)
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_user_shares us ON us.group_id = m.group_id
            WHERE m.contact_id = c.id AND us.user_id = $2)
    )
)
SELECT c.*, co.name AS company_name
FROM contacts c
LEFT JOIN contacts co ON co.id = c.company_id
WHERE c.id = $1 AND c.tenant_id = $3
  AND (c.id IN (SELECT id FROM vis) OR c.company_id IN (SELECT id FROM vis));

-- name: ListCompanyPeople :many
-- People belonging to a company. No visibility filter: whoever reached the company's record
-- already went through GetContact — and its people are part of that same record.
SELECT * FROM contacts WHERE company_id = $1 AND tenant_id = $2 ORDER BY name;

-- name: FindCompanyByDomain :one
-- Company (kind='company') owned by this user with this domain — the basis for the automatic person↔company link.
SELECT * FROM contacts
WHERE tenant_id = @tenant_id AND owner_user_id = @owner_user_id
  AND kind = 'company' AND lower(domain) = lower(@domain) AND domain <> ''
ORDER BY created_at LIMIT 1;

-- name: LinkPeopleByDomain :execrows
-- Links people without a company to a company by domain (idempotent). Used by "enrich now".
UPDATE contacts p SET company_id = c.id
FROM contacts c
WHERE p.tenant_id = @tenant_id AND p.owner_user_id = @owner_user_id
  AND p.kind = 'person' AND p.company_id IS NULL AND p.domain <> ''
  AND c.tenant_id = p.tenant_id AND c.owner_user_id = p.owner_user_id
  AND c.kind = 'company' AND lower(c.domain) = lower(p.domain);

-- name: CreateContact :one
INSERT INTO contacts (tenant_id, owner_user_id, kind, name, email, phone, company, domain, notes,
                      tags, title, mobile, is_whatsapp, allow_phone, allow_email, company_id,
                      extra_emails, extra_phones, custom_fields)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19) RETURNING *;

-- name: UpdateContact :one
-- EDITING is owner-only (viewing ≠ modifying).
UPDATE contacts SET kind = $3, name = $4, email = $5, phone = $6, company = $7, domain = $8,
    notes = $9, tags = $10, title = $11, mobile = $12, is_whatsapp = $13, allow_phone = $14,
    allow_email = $15, company_id = $16, extra_emails = $17, extra_phones = $18, custom_fields = $19
WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: SetContactAvatar :one
-- Sets/removes the contact's photo (points to one of the owner's storage_files). Owner only.
UPDATE contacts SET avatar_file_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteContact :exec
DELETE FROM contacts WHERE id = $1 AND owner_user_id = $2;

-- name: MergeReassignNotes :exec
UPDATE crm_notes SET contact_id = sqlc.arg(keep) WHERE contact_id = sqlc.arg(merge) AND tenant_id = sqlc.arg(tenant);

-- name: MergeReassignGroups :exec
-- Moves the group memberships (avoids duplicating the contact_id+group_id PK via ON CONFLICT).
INSERT INTO contact_group_members (contact_id, group_id)
SELECT sqlc.arg(keep), m.group_id FROM contact_group_members m WHERE m.contact_id = sqlc.arg(merge)
ON CONFLICT DO NOTHING;

-- name: FindContactByEmail :one
SELECT * FROM contacts WHERE tenant_id = $2 AND $1 <> '' AND lower(email) = lower($1) LIMIT 1;

-- ---- Sharing a CONTACT GROUP with a TEAM ----

-- name: ListGroupShares :many
SELECT s.user_group_id, u.name FROM contact_group_shares s
JOIN user_groups u ON u.id = s.user_group_id WHERE s.group_id = $1 ORDER BY u.name;

-- name: SetGroupShare :exec
INSERT INTO contact_group_shares (group_id, user_group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ClearGroupShares :exec
DELETE FROM contact_group_shares WHERE group_id = $1;

-- Sharing a CONTACT GROUP with a specific USER (in addition to teams).
-- name: ListGroupUserShares :many
SELECT s.user_id, u.name FROM contact_group_user_shares s
JOIN mail_users u ON u.id = s.user_id WHERE s.group_id = $1 ORDER BY u.name;

-- name: SetGroupUserShare :exec
INSERT INTO contact_group_user_shares (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ClearGroupUserShares :exec
DELETE FROM contact_group_user_shares WHERE group_id = $1;

-- Tenant's teams (id+name) for the share PICKER — without requiring admin permission, unlike
-- ListUserGroups (which is for member management). Choosing who to share my own group with is
-- not an administration operation.
-- name: ListTeamsForShare :many
SELECT id, name FROM user_groups WHERE tenant_id = $1 ORDER BY name;

-- name: ListContactNotes :many
SELECT n.id, n.body, n.created_at, u.name AS author_name, u.email AS author_email
FROM crm_notes n JOIN mail_users u ON u.id = n.author_id
WHERE n.contact_id = $1 AND n.tenant_id = $2 ORDER BY n.created_at DESC;

-- name: CreateContactNote :one
INSERT INTO crm_notes (tenant_id, contact_id, author_id, body)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DeleteCrmNote :exec
DELETE FROM crm_notes WHERE id = $1 AND author_id = $2;

-- name: ListContactGroups :many
SELECT * FROM contact_groups WHERE owner_user_id = $1 ORDER BY name;

-- name: CreateContactGroup :one
INSERT INTO contact_groups (tenant_id, owner_user_id, name, color) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateContactGroup :one
UPDATE contact_groups SET name = $3, color = $4 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteContactGroup :exec
DELETE FROM contact_groups WHERE id = $1 AND owner_user_id = $2;

-- name: ClearContactGroups :exec
DELETE FROM contact_group_members WHERE contact_id = $1;

-- name: ListContactGroupEmails :many
-- Emails of a group's contacts (distribution list): write to the whole group at once.
-- Only the owner's own contacts, and only those with an email filled in.
SELECT c.name, c.email FROM contact_group_members m
JOIN contacts c ON c.id = m.contact_id
WHERE m.group_id = $1 AND c.owner_user_id = $2 AND c.email <> '' ORDER BY c.name;

-- name: AddContactToGroup :exec
INSERT INTO contact_group_members (contact_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListContactMemberships :many
SELECT m.contact_id, m.group_id FROM contact_group_members m
JOIN contacts c ON c.id = m.contact_id
WHERE c.owner_user_id = $1;

-- ===== Billing and quotas =====

-- name: CountMailboxesByTenant :one
-- Counts mailboxes AND aliases: an alias is billed as a mailbox. This is the count that feeds
-- the plan limit, the billed seats, and the mailbox cap, so aliases must be included here and
-- not only in the usage report.
SELECT (SELECT count(*) FROM mail_users u WHERE u.tenant_id = $1)
     + (SELECT count(*) FROM email_aliases a WHERE a.tenant_id = $1);

-- name: SumStorageBytes :one
SELECT COALESCE(sum(size), 0)::bigint
FROM (SELECT DISTINCT ON (blob_key) size FROM storage_files WHERE tenant_id = $1) t;

-- name: UpdateTenantPlan :exec
UPDATE tenants SET plan = $2 WHERE id = $1;





-- name: GetTenantOwner :one
-- Organization owner: who receives the alerts when no billing contact is set.
SELECT * FROM mail_users WHERE tenant_id = $1 AND role = 'owner' ORDER BY created_at ASC LIMIT 1;

-- name: GetBillingProfile :one
SELECT * FROM billing_profiles WHERE tenant_id = $1;

-- name: UpsertBillingProfile :one
-- stripe_customer does NOT go in here: it is written only by checkout (SetBillingStripeCustomer).
-- If it were included in the upsert, saving the address would wipe out the link to the Stripe customer.
INSERT INTO billing_profiles (
    tenant_id, kind, legal_name, tax_id, country, city, postal_code,
    address_line1, address_line2, invoice_email, invoice_name, phone, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
ON CONFLICT (tenant_id) DO UPDATE SET
    kind = EXCLUDED.kind, legal_name = EXCLUDED.legal_name, tax_id = EXCLUDED.tax_id,
    country = EXCLUDED.country, city = EXCLUDED.city, postal_code = EXCLUDED.postal_code,
    address_line1 = EXCLUDED.address_line1, address_line2 = EXCLUDED.address_line2,
    invoice_email = EXCLUDED.invoice_email, invoice_name = EXCLUDED.invoice_name,
    phone = EXCLUDED.phone, updated_at = now()
RETURNING *;

-- name: SetBillingStripeCustomer :exec
UPDATE billing_profiles SET stripe_customer = @stripe_customer, updated_at = now() WHERE tenant_id = @tenant_id;

-- name: MarkWebhookProcessed :one
INSERT INTO processed_webhooks (event_id) VALUES ($1) ON CONFLICT DO NOTHING RETURNING event_id;

-- ===== Meet (meetings + participants) =====

-- name: SetRSVP :one
UPDATE meeting_participants SET rsvp = $3
WHERE meeting_id = $1 AND email = $2 RETURNING *;

-- name: CreateNotification :one
INSERT INTO notifications (tenant_id, user_id, kind, title, body, link, meeting_id, args)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationsReadByKind :exec
UPDATE notifications SET read_at = now() WHERE user_id = $1 AND kind = $2 AND read_at IS NULL;

-- ===== Meeting recordings (LiveKit Egress) =====

-- name: CreateRecording :one
INSERT INTO recordings (tenant_id, room_name, meeting_id, egress_id, blob_key, started_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetRecordingByEgress :one
SELECT * FROM recordings WHERE egress_id = $1;

-- name: SetRecordingReady :exec
UPDATE recordings SET status = 'ready', ended_at = now(), duration_seconds = $2, size_bytes = $3
WHERE egress_id = $1;

-- name: SetRecordingFailed :exec
UPDATE recordings SET status = 'failed', ended_at = now() WHERE egress_id = $1;

-- name: ListRecordingsForTenant :many
SELECT * FROM recordings WHERE tenant_id = $1 ORDER BY started_at DESC LIMIT $2;

-- name: GetRecordingInTenant :one
SELECT * FROM recordings WHERE id = $1 AND tenant_id = $2;

-- name: DeleteRecording :exec
DELETE FROM recordings WHERE id = $1 AND tenant_id = $2;

-- name: GetActiveRecordingForRoom :one
SELECT * FROM recordings WHERE room_name = $1 AND status = 'recording' ORDER BY started_at DESC LIMIT 1;

-- ===== Meet: lobby (waiting room / approval) =====

-- name: CreateKnock :one
INSERT INTO meeting_lobby (meeting_id, tenant_id, room_name, name, identity)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetKnock :one
SELECT * FROM meeting_lobby WHERE id = $1;

-- name: ListPendingKnocks :many
SELECT * FROM meeting_lobby WHERE meeting_id = $1 AND status = 'pending' ORDER BY created_at;

-- name: SetKnockStatus :one
UPDATE meeting_lobby SET status = $2 WHERE id = $1 RETURNING *;

-- ===== Tags (shared catalog) =====

-- name: ListTags :many
SELECT * FROM tags WHERE tenant_id = $1 ORDER BY name;

-- name: UpsertTag :one
INSERT INTO tags (tenant_id, name, color) VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, name) DO UPDATE SET color = EXCLUDED.color RETURNING *;

-- name: DeleteTag :exec
DELETE FROM tags WHERE id = $1 AND tenant_id = $2;

-- name: EventsOverlapping :many
SELECT * FROM events WHERE owner_user_id = $1 AND starts_at < $3 AND ends_at > $2 ORDER BY starts_at;

-- name: EventsOverlappingExcept :many
-- Like EventsOverlapping, but ignores one event (the one being edited) so it doesn't conflict with itself.
SELECT * FROM events WHERE owner_user_id = $1 AND starts_at < $3 AND ends_at > $2 AND id <> $4 ORDER BY starts_at;

-- name: CreateChannel :one
INSERT INTO channels (tenant_id, kind, name, topic, dm_key, created_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetChannel :one
SELECT * FROM channels WHERE id = $1 AND tenant_id = $2;

-- name: GetGeneralChannel :one
SELECT * FROM channels WHERE tenant_id = $1 AND is_general;

-- name: CreateGeneralChannel :one
-- Tenant's default channel. The partial index guarantees only one per tenant; in a race, the
-- second INSERT returns no row and the caller reads back whatever ended up there.
INSERT INTO channels (tenant_id, kind, name, topic, dm_key, created_by, is_general)
VALUES ($1, 'public', $2, '', '', $3, true)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetDMByKey :one
SELECT * FROM channels WHERE tenant_id = $1 AND kind = 'dm' AND dm_key = $2;

-- name: ListChannelsForUser :many
SELECT c.id, c.tenant_id, c.kind, c.name, c.topic, c.dm_key, c.created_by, c.created_at,
       c.listed, c.add_policy, c.is_general, cm.role, cm.notif_pref, cm.last_read_at,
       (SELECT count(*) FROM messages m
          WHERE m.channel_id = c.id AND m.created_at > cm.last_read_at
            AND m.user_id <> $1 AND m.deleted_at IS NULL) AS unread
FROM channels c
JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = $1
WHERE c.tenant_id = $2 AND c.archived_at IS NULL
ORDER BY c.kind, c.name;

-- name: ListPublicChannels :many
-- Only public AND listed channels show up in discovery. Unlisted = closed (invite-only).
SELECT c.* FROM channels c
WHERE c.tenant_id = $1 AND c.kind = 'public' AND c.listed = true AND c.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = $2)
ORDER BY c.name;

-- name: SetChannelArchived :one
-- Archives (true) or restores (false). Archived leaves the lists and becomes read-only; the
-- history stays intact, unlike deleting.
UPDATE channels SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN now() ELSE NULL END
WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: ListArchivedChannels :many
-- Only lists it for someone who CAN restore it: organization admin or channel owner. That way
-- everything shown is actionable, and for everyone else the archived channel disappears (which is the intended effect).
SELECT c.* FROM channels c
WHERE c.tenant_id = @tenant_id AND c.archived_at IS NOT NULL AND c.kind <> 'dm'
  AND (
      sqlc.arg(is_admin)::boolean
      OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = @user_id AND cm.role = 'owner')
  )
ORDER BY c.archived_at DESC;

-- name: UpdateChannelAccess :exec
UPDATE channels SET listed = $3, add_policy = $4 WHERE id = $1 AND tenant_id = $2;

-- name: UpdateChannelSettings :exec
UPDATE channels SET name = $3, topic = $4, listed = $5, add_policy = $6 WHERE id = $1 AND tenant_id = $2;

-- name: UpdateMemberRole :exec
UPDATE channel_members SET role = $3 WHERE channel_id = $1 AND user_id = $2;

-- name: AddChannelMember :exec
INSERT INTO channel_members (channel_id, tenant_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id, user_id) DO NOTHING;

-- name: RemoveChannelMember :exec
DELETE FROM channel_members WHERE channel_id = $1 AND user_id = $2;

-- name: GetMembership :one
SELECT * FROM channel_members WHERE channel_id = $1 AND user_id = $2;

-- name: ListChannelMembers :many
SELECT cm.user_id, cm.role, cm.last_read_at, u.email, u.name, u.status_emoji, u.status_text
FROM channel_members cm JOIN mail_users u ON u.id = cm.user_id
WHERE cm.channel_id = $1 ORDER BY u.name;

-- name: GetChannelMeta :one
-- Channel metadata for deciding the message notification (a DM always notifies; a channel by name).
SELECT kind, name FROM channels WHERE id = $1;

-- name: ListChannelMemberPrefs :many
-- Channel members + notification preference, for the "new message" fan-out.
SELECT user_id, notif_pref FROM channel_members WHERE channel_id = $1;

-- name: ListMemberUserIDs :many
SELECT user_id FROM channel_members WHERE channel_id = $1;

-- name: UpdateLastRead :exec
UPDATE channel_members SET last_read_at = now() WHERE channel_id = $1 AND user_id = $2;

-- name: CreateMessage :one
INSERT INTO messages (tenant_id, channel_id, user_id, parent_id, body)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListMessagesByChannel :many
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name,
       (SELECT count(*) FROM messages r WHERE r.parent_id = m.id AND r.deleted_at IS NULL) AS reply_count, m.bot_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.parent_id IS NULL
ORDER BY m.created_at DESC LIMIT $2;

-- name: ListMessagesBeforeByChannel :many
-- History page BEFORE a given instant (cursor on created_at) — loads older messages.
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name,
       (SELECT count(*) FROM messages r WHERE r.parent_id = m.id AND r.deleted_at IS NULL) AS reply_count, m.bot_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.parent_id IS NULL AND m.created_at < $2
ORDER BY m.created_at DESC LIMIT $3;

-- name: SaveMessage :exec
INSERT INTO saved_messages (user_id, message_id, tenant_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: UnsaveMessage :exec
DELETE FROM saved_messages WHERE user_id = $1 AND message_id = $2;

-- name: ListSavedIDsInChannel :many
-- IDs of the messages the user saved WITHIN a channel (marks the state in the message list).
SELECT sm.message_id FROM saved_messages sm JOIN messages m ON m.id = sm.message_id
WHERE sm.user_id = $1 AND m.channel_id = $2;

-- name: ListSavedMessages :many
-- User's saved messages (the "Saved" panel), with author and channel, most recent first.
SELECT m.id, m.channel_id, m.body, m.created_at, c.kind AS channel_kind, c.name AS channel_name,
       u.email AS author_email, u.name AS author_name, sm.created_at AS saved_at
FROM saved_messages sm
JOIN messages m ON m.id = sm.message_id
JOIN channels c ON c.id = m.channel_id
JOIN mail_users u ON u.id = m.user_id
WHERE sm.user_id = $1 AND m.deleted_at IS NULL
ORDER BY sm.created_at DESC LIMIT 50;

-- name: ListMessagesAfterByChannel :many
-- Messages AFTER a given instant (the new side of the "jump to message" window), in order.
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name,
       (SELECT count(*) FROM messages r WHERE r.parent_id = m.id AND r.deleted_at IS NULL) AS reply_count, m.bot_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.parent_id IS NULL AND m.created_at > $2
ORDER BY m.created_at ASC LIMIT $3;

-- name: ListThreadReplies :many
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.parent_id = $1
ORDER BY m.created_at ASC;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = $1 AND tenant_id = $2;

-- name: EditMessage :one
UPDATE messages SET body = $3, edited_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL RETURNING *;

-- name: DeleteMessage :exec
UPDATE messages SET deleted_at = now(), body = '' WHERE id = $1 AND user_id = $2;

-- name: DeleteMessageAny :exec
-- Soft-deletes by id, without requiring authorship — for moderation by a manager/channel owner.
UPDATE messages SET deleted_at = now(), body = '' WHERE id = $1 AND tenant_id = $2;

-- name: DeleteChannel :exec
-- Deletes the channel (cascade removes members/messages). Owner only, checked in the handler.
DELETE FROM channels WHERE id = $1 AND tenant_id = $2;

-- name: SetLastReadAt :exec
-- Sets the read marker to a specific instant (used by "mark as unread").
UPDATE channel_members SET last_read_at = $3 WHERE channel_id = $1 AND user_id = $2;

-- name: AddReaction :exec
INSERT INTO message_reactions (message_id, user_id, emoji)
VALUES ($1, $2, $3) ON CONFLICT (message_id, user_id, emoji) DO NOTHING;

-- name: RemoveReaction :exec
DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3;

-- name: ListReactionsByChannel :many
SELECT r.message_id, r.emoji, r.user_id
FROM message_reactions r JOIN messages m ON m.id = r.message_id
WHERE m.channel_id = $1;

-- name: AddMessageAttachment :exec
INSERT INTO message_attachments (message_id, file_id, name, size)
VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING;

-- name: ListAttachmentsByChannel :many
-- A deleted message's attachments do not come back (the message becomes just the "deleted" marker).
SELECT a.message_id, a.file_id, a.name, a.size
FROM message_attachments a JOIN messages m ON m.id = a.message_id
WHERE m.channel_id = $1 AND m.deleted_at IS NULL;

-- name: GetChatAttachment :one
SELECT sf.blob_key, a.name
FROM message_attachments a
JOIN messages m ON m.id = a.message_id
JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = $2
JOIN storage_files sf ON sf.id = a.file_id
WHERE a.file_id = $1 AND m.deleted_at IS NULL
LIMIT 1;

-- name: SearchMessages :many
-- Message search (FTS via body_tsv) with optional Slack-style filters: from:<author>,
-- before:/after:<date>. The flags turn each filter on; an empty term means filters only.
SELECT m.id, m.channel_id, c.kind AS channel_kind, c.name AS channel_name,
       m.user_id, m.body, m.created_at, u.name AS author_name, u.email AS author_email
FROM messages m
JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = sqlc.arg(user_id)
JOIN channels c ON c.id = m.channel_id
JOIN mail_users u ON u.id = m.user_id
WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.deleted_at IS NULL
  AND (sqlc.arg(term)::text = '' OR m.body_tsv @@ plainto_tsquery('simple', sqlc.arg(term)))
  AND (NOT sqlc.arg(has_from)::boolean OR m.user_id = sqlc.arg(from_user))
  AND (NOT sqlc.arg(has_before)::boolean OR m.created_at < sqlc.arg(before_ts))
  AND (NOT sqlc.arg(has_after)::boolean OR m.created_at > sqlc.arg(after_ts))
ORDER BY m.created_at DESC LIMIT sqlc.arg(lim);

-- name: CreateUserGroup :one
INSERT INTO user_groups (tenant_id, name, created_by) VALUES ($1, $2, $3) RETURNING *;

-- name: SetUserGroupChannel :exec
UPDATE user_groups SET channel_id = $2 WHERE id = $1;

-- name: ListUserGroups :many
SELECT g.*, (SELECT count(*) FROM user_group_members m WHERE m.group_id = g.id) AS members
FROM user_groups g WHERE g.tenant_id = $1 ORDER BY g.name;

-- name: GetUserGroup :one
SELECT * FROM user_groups WHERE id = $1 AND tenant_id = $2;

-- name: UpdateUserGroup :one
UPDATE user_groups SET name = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteUserGroup :exec
DELETE FROM user_groups WHERE id = $1 AND tenant_id = $2;

-- name: AddUserToGroup :exec
INSERT INTO user_group_members (group_id, user_id, tenant_id) VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveUserFromGroup :exec
DELETE FROM user_group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.id, u.email, u.name, u.role
FROM user_group_members m JOIN mail_users u ON u.id = m.user_id
WHERE m.group_id = $1 ORDER BY u.name, u.email;

-- name: ListGroupsForUser :many
SELECT g.id, g.name FROM user_group_members m JOIN user_groups g ON g.id = m.group_id
WHERE m.user_id = $1 ORDER BY g.name;

-- name: UpdateChannelName :one
UPDATE channels SET name = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: ListContactGroupsOf :many
SELECT group_id FROM contact_group_members WHERE contact_id = $1;

-- name: ListColleagues :many
-- Colleagues in the same tenant: only the minimum needed to assign work (id/name/email). Does
-- NOT expose role, MFA, or anything from the admin panel — that's why it doesn't need members.view.
SELECT id, email, name, avatar FROM mail_users WHERE tenant_id = $1 ORDER BY name, email;

-- name: GetContactGroupOwned :one
-- Contact group belonging to the owner themselves — used before touching its sharing.
SELECT * FROM contact_groups WHERE id = $1 AND owner_user_id = $2;

-- ---- Card comments ----

-- name: ListCommentAuthors :many
-- Who has already commented on this card — they are the ones interested in the conversation.
SELECT DISTINCT author_id FROM card_comments WHERE card_id = $1 AND tenant_id = $2;

-- name: DetachFile :exec
DELETE FROM card_attachments WHERE id = $1 AND tenant_id = $2;

-- name: ListGifs :many
SELECT g.id, g.file_id, g.created_at,
       COALESCE(NULLIF(g.name, ''), f.name) AS name, f.size AS size
FROM gif_library g JOIN storage_files f ON f.id = g.file_id
WHERE g.owner_user_id = $1 ORDER BY g.created_at DESC LIMIT 60;

-- name: AddGif :one
-- ON CONFLICT: saving the same GIF twice is neither an error nor a duplicate — it's the same drawer.
INSERT INTO gif_library (tenant_id, owner_user_id, file_id, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (owner_user_id, file_id) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: DeleteGif :exec
DELETE FROM gif_library WHERE id = $1 AND owner_user_id = $2;

-- name: GetUserSettings :one
SELECT settings FROM mail_users WHERE id = $1;

-- name: UpdateUserSettings :exec
-- Top-level merge (||): saving {"email":{…}} does not erase {"chat":{…}}. Each app writes its own
-- section without stepping on the others.
UPDATE mail_users SET settings = settings || $2 WHERE id = $1;

-- name: UpsertPushSubscription :exec
-- ON CONFLICT: reactivating push on the same browser reuses the row (the keys may rotate).
INSERT INTO push_subscriptions (tenant_id, user_id, endpoint, p256dh, auth)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth;

-- name: ListPushSubscriptionsByUser :many
SELECT * FROM push_subscriptions WHERE user_id = $1;

-- name: DeletePushSubscriptionByEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: GetFileLinkByFile :one
-- Reuses only a link that is still valid (not expired); an expired one counts as nonexistent.
SELECT * FROM file_links WHERE file_id = $1 AND (expires_at IS NULL OR expires_at > now());

-- name: CreateFileLink :one
INSERT INTO file_links (tenant_id, file_id, token, created_by, expires_at, password_hash, permission, allow_download)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateFileLink :one
-- Reconfigures an existing link (expiry, password, permission, download) without changing the token. Owner only.
UPDATE file_links SET expires_at = $2, password_hash = $3, permission = $5, allow_download = $6
WHERE file_id = $1 AND tenant_id = $4 RETURNING *;

-- name: DeleteFileLinkByFile :exec
-- Revokes a file's public link (owner's own).
DELETE FROM file_links WHERE file_id = $1 AND tenant_id = $2;

-- name: GetFileByLinkToken :one
-- Resolves the public token → file (+ the link's password hash). No tenant/owner filter: the token
-- IS the authorization. But the link must still be within its validity (expires_at in the future or NULL).
SELECT f.*, l.password_hash, l.permission, l.allow_download FROM file_links l JOIN storage_files f ON f.id = l.file_id
WHERE l.token = $1 AND (l.expires_at IS NULL OR l.expires_at > now());

-- name: ListFilesSharedWithMe :many
-- Files another person in the tenant shared with me (principal = my email). DISTINCT because
-- the same person may have shared the file more than once.
SELECT DISTINCT f.* FROM storage_shares sh
JOIN storage_files f ON f.id = sh.file_id
WHERE sh.tenant_id = $1 AND sh.principal = $2
ORDER BY f.created_at DESC;

-- name: GetFileIfSharedWithMe :one
-- The file, if it was shared with me (principal = my email, same tenant). Authorizes the download
-- for someone who is not the owner but received the share.
SELECT f.* FROM storage_shares sh JOIN storage_files f ON f.id = sh.file_id
WHERE f.id = $1 AND sh.tenant_id = $2 AND sh.principal = $3;

-- name: UpdateTenantBranding :exec
UPDATE tenants SET brand_color = $2, brand_logo_white_dark = @brand_logo_white_dark, brand_logo = $3 WHERE id = $1;

-- name: GetTenantDisabledApps :one
SELECT disabled_apps FROM tenants WHERE id = $1;

-- name: SetTenantDisabledApps :exec
UPDATE tenants SET disabled_apps = $2 WHERE id = $1;

-- name: GetTenantBrandingByEmail :one
-- Organization's brand from a user's email (for the login screen, pre-auth).
SELECT t.name, t.brand_color, t.brand_logo
FROM mail_users u JOIN tenants t ON t.id = u.tenant_id
WHERE u.email = $1;

-- name: ListAnnouncements :many
-- Tenant announcements: search (title/body), pagination and visibility. $5 = also see the
-- unpublished ones (admin only); members see only the published ones.
SELECT * FROM announcements
WHERE tenant_id = $1
  AND ($2 = '' OR title ILIKE '%' || $2 || '%' OR body ILIKE '%' || $2 || '%')
  AND (published OR $5)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAnnouncements :one
SELECT count(*) FROM announcements
WHERE tenant_id = $1
  AND ($2 = '' OR title ILIKE '%' || $2 || '%' OR body ILIKE '%' || $2 || '%')
  AND (published OR $3);

-- name: CreateAnnouncement :one
INSERT INTO announcements (tenant_id, author_id, author_name, title, body, published)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: SetAnnouncementPublished :exec
UPDATE announcements SET published = $3 WHERE id = $1 AND tenant_id = $2;

-- name: DeleteAnnouncement :exec
DELETE FROM announcements WHERE id = $1 AND tenant_id = $2;

-- name: SearchChannelMembers :many
-- Members of a channel, with optional search by name/email and pagination (for large channels).
SELECT cm.user_id, cm.role, cm.last_read_at, u.email, u.name, u.status_emoji, u.status_text
FROM channel_members cm JOIN mail_users u ON u.id = cm.user_id
WHERE cm.channel_id = $1
  AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.email ILIKE '%' || $2 || '%')
ORDER BY u.name
LIMIT $3 OFFSET $4;

-- name: CountChannelMembers :one
SELECT count(*) FROM channel_members cm JOIN mail_users u ON u.id = cm.user_id
WHERE cm.channel_id = $1
  AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.email ILIKE '%' || $2 || '%');

-- name: UpdateChannelCanvas :exec
UPDATE channels SET canvas = $3, canvas_updated_at = now() WHERE id = $1 AND tenant_id = $2;

-- name: SetMessagePinned :exec
UPDATE messages SET pinned_at = CASE WHEN $2::boolean THEN now() ELSE NULL END WHERE id = $1;

-- name: ListPinnedByChannel :many
SELECT m.id, m.body, m.created_at, m.pinned_at, u.email AS author_email, u.name AS author_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.pinned_at IS NOT NULL AND m.deleted_at IS NULL
ORDER BY m.pinned_at DESC;

-- name: ListChannelBookmarks :many
SELECT * FROM channel_bookmarks WHERE channel_id = $1 ORDER BY created_at DESC;

-- name: CreateChannelBookmark :one
INSERT INTO channel_bookmarks (channel_id, tenant_id, kind, title, url, file_id, file_name, file_size, added_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: GetChannelBookmark :one
SELECT b.*, sf.blob_key FROM channel_bookmarks b
LEFT JOIN storage_files sf ON sf.id = b.file_id
WHERE b.id = $1 AND b.channel_id = $2;

-- name: DeleteChannelBookmark :exec
DELETE FROM channel_bookmarks WHERE id = $1 AND channel_id = $2;

-- name: SetChannelNotifPref :exec
UPDATE channel_members SET notif_pref = $3 WHERE channel_id = $1 AND user_id = $2;

-- name: SearchMessagesInChannel :many
SELECT m.id, m.body, m.created_at, u.email AS author_email, u.name AS author_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.deleted_at IS NULL AND m.body_tsv @@ plainto_tsquery('simple', $2)
ORDER BY m.created_at DESC LIMIT 40;

-- name: SetUserStatus :exec
UPDATE mail_users SET status_emoji = $2, status_text = $3 WHERE id = $1;

-- name: GetUserStatusByEmail :one
SELECT status_emoji, status_text FROM mail_users WHERE email = $1 AND tenant_id = $2;

-- name: CreateSnooze :one
INSERT INTO snoozed_emails (tenant_id, mail_user_id, email_id, wake_at) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DueSnoozes :many
-- Snoozes that are due (wake_at has passed) and not yet processed, with the owner's encrypted
-- JMAP credential so the worker can move the message back to the Inbox.
SELECT s.id, s.email_id, u.jmap_cred FROM snoozed_emails s
JOIN mail_users u ON u.id = s.mail_user_id
WHERE NOT s.done AND s.wake_at <= now() ORDER BY s.wake_at LIMIT 200;

-- name: MarkSnoozeDone :exec
UPDATE snoozed_emails SET done = true WHERE id = $1;

-- name: UsersForMailPush :many
-- Users eligible for new-email push: they have a JMAP credential (brokering), at least one push
-- subscription, and are not in Do Not Disturb. mail_seen_at is the basis for the last check.
SELECT DISTINCT u.id, u.tenant_id, u.jmap_cred, u.mail_seen_at
FROM mail_users u JOIN push_subscriptions p ON p.user_id = u.id
WHERE u.jmap_cred <> '' AND u.presence <> 'dnd';

-- name: UpdateMailSeen :exec
UPDATE mail_users SET mail_seen_at = $2 WHERE id = $1;

-- name: AddEventInvite :exec
INSERT INTO event_invites (event_id, tenant_id, user_id) VALUES ($1, $2, $3) ON CONFLICT (event_id, user_id) DO NOTHING;

-- name: ClearEventInvites :exec
DELETE FROM event_invites WHERE event_id = $1;

-- name: ListEventInviteesForEvents :many
-- Guests for multiple events in a single round trip, used to render avatars on calendar event cards.
SELECT i.event_id, i.status, u.name, u.email, u.avatar
FROM event_invites i JOIN mail_users u ON u.id = i.user_id
WHERE i.event_id = ANY(@event_ids::uuid[]) ORDER BY u.name;

-- name: ListEventInvites :many
SELECT i.user_id, i.status, u.name, u.email FROM event_invites i JOIN mail_users u ON u.id = i.user_id WHERE i.event_id = $1 ORDER BY u.name;

-- name: SetInviteStatus :exec
UPDATE event_invites SET status = $3 WHERE event_id = $1 AND user_id = $2;

-- name: GetEventInvite :one
SELECT * FROM event_invites WHERE event_id = $1 AND user_id = $2;

-- name: ListInvitedEvents :many
-- Events the user was invited to (with their status), so they show up on their calendar.
SELECT e.*, i.status AS my_rsvp FROM event_invites i JOIN events e ON e.id = i.event_id WHERE i.user_id = $1 AND e.deleted_at IS NULL;

-- name: GetEventInTenant :one
SELECT * FROM events WHERE id = $1 AND tenant_id = $2;

-- name: SetEventTimes :exec
-- Syncs the calendar event linked to the meeting when it is rescheduled (re-arms the reminder).
UPDATE events SET starts_at = $2, ends_at = $3, reminded = false WHERE id = $1;

-- ===== Custom workspace emojis =====
-- name: ListWorkspaceEmojis :many
SELECT e.id, e.name, e.file_id, e.created_by FROM workspace_emojis e WHERE e.tenant_id = $1 ORDER BY e.name;

-- name: CreateWorkspaceEmoji :one
INSERT INTO workspace_emojis (tenant_id, name, file_id, created_by) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DeleteWorkspaceEmoji :exec
DELETE FROM workspace_emojis WHERE id = $1 AND tenant_id = $2;

-- ===== Scheduled messages + message reminders =====
-- name: CreateScheduledMessage :one
INSERT INTO scheduled_messages (tenant_id, channel_id, user_id, body, send_at)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListScheduledMessages :many
SELECT sm.*, c.name AS channel_name FROM scheduled_messages sm JOIN channels c ON c.id = sm.channel_id
WHERE sm.user_id = $1 ORDER BY sm.send_at;

-- name: DeleteScheduledMessage :exec
DELETE FROM scheduled_messages WHERE id = $1 AND user_id = $2;

-- name: DueScheduledMessages :many
SELECT sm.*, COALESCE(NULLIF(u.name, ''), u.email) AS author_name, u.email AS author_email
FROM scheduled_messages sm JOIN mail_users u ON u.id = sm.user_id
WHERE sm.send_at <= now() ORDER BY sm.send_at LIMIT 100;

-- name: PurgeScheduledMessage :exec
DELETE FROM scheduled_messages WHERE id = $1;

-- name: CreateMessageReminder :one
INSERT INTO message_reminders (tenant_id, user_id, channel_id, message_id, note, remind_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: DueMessageReminders :many
SELECT mr.*, c.name AS channel_name FROM message_reminders mr JOIN channels c ON c.id = mr.channel_id
WHERE mr.remind_at <= now() ORDER BY mr.remind_at LIMIT 100;

-- name: PurgeMessageReminder :exec
DELETE FROM message_reminders WHERE id = $1;

-- ===== Multiple calendars + free-busy =====
-- name: ListCalendars :many
SELECT * FROM calendars WHERE owner_user_id = $1 ORDER BY created_at;

-- name: CreateCalendar :one
INSERT INTO calendars (tenant_id, owner_user_id, name, color) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateCalendar :one
UPDATE calendars SET name = $3, color = $4 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteCalendar :exec
DELETE FROM calendars WHERE id = $1 AND owner_user_id = $2;

-- name: FreeBusy :many
-- A colleague's busy blocks (start/end only, no details) within a range. The handler validates
-- that the target is in the same tenant. Ignores trashed events.
SELECT starts_at, ends_at FROM events
WHERE owner_user_id = $1 AND tenant_id = $2 AND deleted_at IS NULL AND all_day = false
  AND starts_at < $4 AND ends_at > $3 ORDER BY starts_at;

-- name: GetCalendar :one
SELECT * FROM calendars WHERE id = $1 AND owner_user_id = $2;

-- ===== CRM: note editing + owner transfer =====
-- name: EditCrmNote :one
-- Only the author edits their own note.
UPDATE crm_notes SET body = $3 WHERE id = $1 AND author_id = $2 RETURNING *;

-- name: TransferContactOwner :one
-- Transfers ownership of the contact: only the current owner can, and only to a colleague in the same tenant (validated in the handler).
UPDATE contacts SET owner_user_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- ===== Board automations =====
-- name: ListAutomations :many
SELECT * FROM board_automations WHERE board_id = $1 ORDER BY created_at;

-- name: CreateAutomation :one
INSERT INTO board_automations (board_id, tenant_id, trigger_column_id, action, action_value)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: DeleteAutomation :exec
DELETE FROM board_automations WHERE id = $1 AND tenant_id = $2;

-- ===== Incoming webhooks (bots) =====
-- name: CreateBotMessage :one
INSERT INTO messages (tenant_id, channel_id, user_id, body, bot_name)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: CreateWebhook :one
INSERT INTO chat_webhooks (tenant_id, channel_id, token, name, created_by)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListWebhooks :many
SELECT * FROM chat_webhooks WHERE channel_id = $1 ORDER BY created_at;

-- name: DeleteWebhook :exec
DELETE FROM chat_webhooks WHERE id = $1 AND tenant_id = $2;

-- name: GetWebhookByToken :one
SELECT * FROM chat_webhooks WHERE token = $1;

-- ===== Server-side send outbox (undo-send + scheduled) =====

-- name: CreateOutbox :one
INSERT INTO outbox_emails (tenant_id, user_id, email_id, identity_id, from_email, rcpts, subject, send_at)
VALUES (@tenant_id, @user_id, @email_id, @identity_id, @from_email, @rcpts, @subject, @send_at)
RETURNING *;

-- name: ListPendingOutbox :many
-- Owner's emails still to be sent (scheduled) — for the management UI. Ordered by send time.
SELECT id, email_id, from_email, rcpts, subject, send_at, status, created_at
FROM outbox_emails
WHERE user_id = @user_id AND tenant_id = @tenant_id AND status = 'pending'
ORDER BY send_at;

-- name: CancelOutbox :execrows
-- Cancels a pending send owned by the user (undo or "cancel scheduled"). Only affects pending ones (idempotent).
UPDATE outbox_emails SET status = 'canceled'
WHERE id = @id AND user_id = @user_id AND tenant_id = @tenant_id AND status = 'pending';

-- name: DueOutbox :many
-- Sends that are due (send_at has passed) and still pending, with the owner's encrypted JMAP
-- credential for the worker to submit (EmailSubmission/set). The limit protects the tick from a giant queue.
SELECT o.id, o.tenant_id, o.email_id, o.identity_id, o.from_email, o.rcpts, u.jmap_cred
FROM outbox_emails o
JOIN mail_users u ON u.id = o.user_id
WHERE o.status = 'pending' AND o.send_at <= now()
ORDER BY o.send_at LIMIT 200;

-- name: MarkOutboxSent :exec
UPDATE outbox_emails SET status = 'sent' WHERE id = $1;

-- name: MarkOutboxFailed :exec
UPDATE outbox_emails SET status = 'failed' WHERE id = $1;

-- ===== Reply templates (canned responses) =====

-- name: ListTemplates :many
SELECT * FROM mail_templates
WHERE owner_user_id = @owner_user_id AND tenant_id = @tenant_id
ORDER BY name;

-- name: CreateTemplate :one
INSERT INTO mail_templates (tenant_id, owner_user_id, name, subject, body_html)
VALUES (@tenant_id, @owner_user_id, @name, @subject, @body_html)
RETURNING *;

-- name: UpdateTemplate :one
UPDATE mail_templates SET name = @name, subject = @subject, body_html = @body_html, updated_at = now()
WHERE id = @id AND owner_user_id = @owner_user_id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteTemplate :execrows
DELETE FROM mail_templates WHERE id = @id AND owner_user_id = @owner_user_id AND tenant_id = @tenant_id;

-- ===== Meeting co-hosts =====

-- name: RecordAssistantToolCall :exec
-- Audit record for a tool call, written for both success and error cases.
-- conversation_id is nullable because earlier data recorded runs with no conversation at all.
INSERT INTO assistant_tool_calls
    (tenant_id, mail_user_id, run_id, conversation_id, agent, tool, args, result, error, duration_ms)
VALUES (@tenant_id, @mail_user_id, @run_id, @conversation_id, @agent, @tool, @args, @result, @error, @duration_ms);

-- name: ListAssistantToolCallsByRun :many
-- Action cards for a run, in chronological order. Filters by tenant (law 5.1).
SELECT * FROM assistant_tool_calls
WHERE run_id = @run_id AND tenant_id = @tenant_id
ORDER BY created_at;

-- name: CreateAssistantConversation :one
-- Opens a conversation. The (tenant, user) pair comes from the session, not the request, so one tenant cannot address another tenant's conversation.
INSERT INTO assistant_conversations (tenant_id, mail_user_id, agent, title)
VALUES (@tenant_id, @mail_user_id, @agent, @title)
RETURNING *;

-- name: GetAssistantConversation :one
-- A conversation belonging to the authenticated user. The double filter (tenant + user) is
-- deliberate: the conversation is personal and is not shared, not even within the same organization (§3.5).
SELECT * FROM assistant_conversations
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: ListAssistantConversations :many
-- The user's own history, most recent first.
SELECT * FROM assistant_conversations
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY updated_at DESC
LIMIT @lim;

-- name: TouchAssistantConversation :exec
-- Bumps the conversation up the list when a new message arrives.
UPDATE assistant_conversations SET updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: DeleteAssistantConversation :exec
-- Deleting takes the messages with it (ON DELETE CASCADE).
DELETE FROM assistant_conversations
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: AppendAssistantMessage :one
-- Appends a message to the thread. `steps` are the response's action cards (§3.3); `agent` is who
-- answered — the routing choice is always visible (§3.2).
INSERT INTO assistant_messages (tenant_id, conversation_id, role, content, steps, stopped, run_id, agent)
VALUES (@tenant_id, @conversation_id, @role, @content, @steps, @stopped, @run_id, @agent)
RETURNING *;

-- name: PinAssistantConversationAgent :exec
-- Pins (or unpins, with an empty value) the agent for the rest of the thread. Only the conversation's owner can do this.
UPDATE assistant_conversations SET pinned_agent = @pinned_agent
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: ListAssistantMessages :many
-- Messages of a conversation, in order. The JOIN guarantees the conversation really belongs to
-- the user — without it, a guessed conversation_id would read someone else's thread.
SELECT m.* FROM assistant_messages m
JOIN assistant_conversations c ON c.id = m.conversation_id
WHERE m.conversation_id = @conversation_id
  AND c.tenant_id = @tenant_id AND c.mail_user_id = @mail_user_id
ORDER BY m.created_at
LIMIT @lim;

-- name: ListClientAgents :many
-- Profiles created by the customer, ordered by name. Filters by tenant (law 5.1).
SELECT * FROM assistant_agents WHERE tenant_id = @tenant_id ORDER BY name;

-- name: GetClientAgent :one
SELECT * FROM assistant_agents WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateClientAgent :one
INSERT INTO assistant_agents (tenant_id, name, purpose, instructions, tools)
VALUES (@tenant_id, @name, @purpose, @instructions, @tools)
RETURNING *;

-- name: UpdateClientAgent :one
UPDATE assistant_agents
SET name = @name, purpose = @purpose, instructions = @instructions, tools = @tools, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteClientAgent :exec
DELETE FROM assistant_agents WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListDisabledBuiltinAgents :many
-- Only what diverges from the default: absence means enabled.
SELECT agent_key FROM assistant_builtin_settings
WHERE tenant_id = @tenant_id AND enabled = FALSE;

-- name: SetBuiltinAgentEnabled :exec
-- Turns a built-in agent on or off for the organization. Controls AVAILABILITY only, never
-- behavior: the instructions and tools remain ours (§3.2).
INSERT INTO assistant_builtin_settings (tenant_id, agent_key, enabled)
VALUES (@tenant_id, @agent_key, @enabled)
ON CONFLICT (tenant_id, agent_key) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: CreateAssistantTask :one
-- Queues a background task. The (tenant, user) pair comes from the session, not the request, so one tenant cannot queue work against another tenant's data.
INSERT INTO assistant_tasks (tenant_id, mail_user_id, conversation_id, agent, message)
VALUES (@tenant_id, @mail_user_id, @conversation_id, @agent, @message)
RETURNING *;

-- name: ListAssistantTasks :many
-- The user's own tasks, most recent first.
SELECT * FROM assistant_tasks
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY created_at DESC
LIMIT @lim;

-- name: GetAssistantTask :one
SELECT * FROM assistant_tasks
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: RequestAssistantTaskCancel :exec
-- Canceling is a REQUEST: the worker sees the flag on the next step. Whatever was already done stays done.
UPDATE assistant_tasks SET cancel_requested = TRUE, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id
  AND status IN ('queued', 'running', 'awaiting_confirmation');

-- name: ClaimAssistantTask :one
-- Claims ONE task from the queue for this worker. SKIP LOCKED lets several workers run without
-- stepping on each other; the lease (claimed_at) is what allows recovery from a worker that dies.
--
-- A 'running' task whose lease is older than the limit becomes eligible again: this is exactly
-- the mid-run restart case (§7.5).
UPDATE assistant_tasks SET status = 'running', claimed_at = now(), updated_at = now()
WHERE id = (
    SELECT id FROM assistant_tasks
    WHERE status = 'queued'
       OR (status IN ('running', 'awaiting_confirmation') AND claimed_at < now() - @lease::interval)
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: HeartbeatAssistantTask :one
-- Renews the lease and returns the task, so the worker can see the cancellation request without a second query.
UPDATE assistant_tasks SET claimed_at = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SaveAssistantTaskProgress :exec
-- Saves progress at each step: this is what allows resuming and what the bubble displays.
UPDATE assistant_tasks SET steps = @steps, status = @status, updated_at = now()
WHERE id = @id;

-- name: FinishAssistantTask :exec
UPDATE assistant_tasks
SET status = @status, answer = @answer, error_code = @error_code, stopped = @stopped,
    steps = @steps, run_id = @run_id, updated_at = now()
WHERE id = @id;

-- name: CreateAssistantPendingAction :one
-- Records an action awaiting approval. Durable: survives a server restart (§6.3).
INSERT INTO assistant_pending_actions (task_id, tenant_id, mail_user_id, tool, args)
VALUES (@task_id, @tenant_id, @mail_user_id, @tool, @args)
RETURNING *;

-- name: GetAssistantPendingAction :one
SELECT * FROM assistant_pending_actions WHERE id = @id;

-- name: ListAssistantPendingActions :many
-- What is waiting on a decision from THIS user.
SELECT * FROM assistant_pending_actions
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id AND decision = ''
ORDER BY created_at;

-- name: DecideAssistantPendingAction :one
-- Decides on ONE action. The `decision = ''` condition makes it single-use: approving twice does
-- not send twice. Filters by user — the approval belongs to whoever owns the action (law 5.1).
UPDATE assistant_pending_actions SET decision = @decision, decided_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id AND decision = ''
RETURNING *;

-- name: GetAssistantTaskEffect :one
-- An effect already produced by this task. This is the idempotency guard for resuming (law 5.3).
SELECT summary FROM assistant_task_effects
WHERE task_id = @task_id AND fingerprint = @fingerprint;

-- name: RecordAssistantTaskEffect :exec
-- Marks the effect as done. ON CONFLICT DO NOTHING because a race between two workers must not
-- turn into an error: what matters is that it ends up marked once.
INSERT INTO assistant_task_effects (task_id, fingerprint, summary)
VALUES (@task_id, @fingerprint, @summary)
ON CONFLICT (task_id, fingerprint) DO NOTHING;

-- name: UpdateStorageFileContentIfVersion :one
-- Saves new content ONLY IF the current version is the expected one (§5.1: edit on top of the
-- version you read). If someone else saved in the meantime, the version no longer matches and no
-- row is updated — the write is refused instead of silently losing someone else's work.
UPDATE storage_files SET blob_key = @blob_key, hash = @hash, size = @size, version = version + 1
WHERE id = @id AND tenant_id = @tenant_id AND owner_user_id = @owner_user_id AND version = @expected_version
RETURNING *;

-- name: ListAssistantMemory :many
-- All of the user's own facts. Small by nature, fits entirely (§8.3).
SELECT * FROM assistant_memory
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY updated_at DESC;

-- name: CreateAssistantMemory :one
-- Stores a fact. The source is always a message from the user (§6.5).
INSERT INTO assistant_memory (tenant_id, mail_user_id, fact, category, source_message_id)
VALUES (@tenant_id, @mail_user_id, @fact, @category, @source_message_id)
RETURNING *;

-- name: UpdateAssistantMemory :one
-- Edits a fact. Contradicting facts replace the previous one (§8.2) — the UI client does this
-- with this update; the source becomes the new message.
UPDATE assistant_memory
SET fact = @fact, category = @category, source_message_id = @source_message_id, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id
RETURNING *;

-- name: DeleteAssistantMemory :exec
DELETE FROM assistant_memory
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: DeleteAllAssistantMemory :exec
-- Deletes everything at once (§8.4). A right of the user (GDPR).
DELETE FROM assistant_memory
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: TouchAssistantMemoryUsed :exec
-- Marks facts as used when they enter a run — feeds the proposal to remove whatever is never used (§8.4).
UPDATE assistant_memory SET last_used_at = now()
WHERE mail_user_id = @mail_user_id AND id = ANY(@ids::uuid[]);

-- name: RecordAssistantUsage :exec
-- Records a turn's tokens for the quota (§9.2).
INSERT INTO assistant_usage (tenant_id, mail_user_id, tokens)
VALUES (@tenant_id, @mail_user_id, @tokens);

-- name: SumAssistantUsageWindow :one
-- Sums the user's tokens over the sliding window. @since is the start of the window (now minus 6h).
SELECT COALESCE(SUM(tokens), 0)::bigint AS total,
       MIN(created_at) AS oldest
FROM assistant_usage
WHERE mail_user_id = @mail_user_id AND created_at >= @since;

-- name: DeleteOldAssistantUsage :exec
-- Clears out whatever has fallen out of the window — the table doesn't need to grow forever.
DELETE FROM assistant_usage WHERE created_at < @before;

-- name: ListExcludedFolders :many
SELECT folder_id FROM assistant_excluded_folders WHERE tenant_id = @tenant_id;

-- name: AddExcludedFolder :exec
INSERT INTO assistant_excluded_folders (tenant_id, folder_id)
VALUES (@tenant_id, @folder_id)
ON CONFLICT (tenant_id, folder_id) DO NOTHING;

-- name: RemoveExcludedFolder :exec
DELETE FROM assistant_excluded_folders WHERE tenant_id = @tenant_id AND folder_id = @folder_id;

-- name: ListAssistantToolCallsByUser :many
-- Audit log of the user's OWN activity, most recent first (§6.4). Readable by them, not only by
-- the administrator — hence filtering by mail_user_id and not only by tenant.
SELECT * FROM assistant_tool_calls
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY created_at DESC
LIMIT @lim;

-- name: AddEventExternal :exec
INSERT INTO event_externals (event_id, tenant_id, email) VALUES ($1, $2, $3)
ON CONFLICT (event_id, email) DO NOTHING;

-- name: DeleteEventExternals :exec
DELETE FROM event_externals WHERE event_id = $1;

-- name: ListEventExternals :many
SELECT email FROM event_externals WHERE event_id = $1 ORDER BY email;

-- name: ListEventExternalsForEvents :many
SELECT event_id, email FROM event_externals WHERE event_id = ANY(@event_ids::uuid[]) ORDER BY email;

-- ===== Booking (Calendly-style public schedule) =====
-- name: UpsertBookingPage :one
INSERT INTO booking_pages (tenant_id, user_id, org_slug, user_slug, enabled, title, duration_min, phone_field, company_field, windows)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (user_id) DO UPDATE SET enabled = $5, title = $6, duration_min = $7, phone_field = $8, company_field = $9, windows = $10
RETURNING *;

-- name: GetBookingPageByUser :one
SELECT * FROM booking_pages WHERE user_id = $1;

-- name: GetBookingPageBySlugs :one
SELECT * FROM booking_pages WHERE org_slug = $1 AND user_slug = $2;

-- name: CreateBooking :one
INSERT INTO bookings (tenant_id, page_id, user_id, name, email, phone, company, note, starts_at, ends_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *;

-- name: SetEventBooking :exec
UPDATE events SET booking_id = $2 WHERE id = $1;

-- name: BookingBusy :many
-- A collaborator's FULL busy time for booking: their own events (timed AND all-day)
-- + events where they are invited and haven't declined. Recurring ones come in via expansion in the handler.
SELECT ev.starts_at, ev.ends_at FROM events ev
WHERE ev.owner_user_id = sqlc.arg(owner_id) AND ev.tenant_id = sqlc.arg(tenant_id) AND ev.deleted_at IS NULL
  AND ev.starts_at < sqlc.arg(to_ts) AND ev.ends_at > sqlc.arg(from_ts)
UNION ALL
SELECT e.starts_at, e.ends_at FROM events e
JOIN event_invites i ON i.event_id = e.id
WHERE i.user_id = sqlc.arg(invitee_id) AND e.tenant_id = sqlc.arg(tenant_id) AND e.deleted_at IS NULL
  AND i.status <> 'declined' AND e.starts_at < sqlc.arg(to_ts) AND e.ends_at > sqlc.arg(from_ts);

-- name: SetEventKind :exec
UPDATE events SET kind = $2 WHERE id = $1 AND tenant_id = $3;

-- name: TimeOffConflicts :many
-- Events that collide with a time-off being scheduled: the user's OWN ones (own=true,
-- cancellable) and the ones where they are invited and haven't declined (own=false, can only be
-- declined). Time-off entries (kind<>'') don't count.
SELECT ev.id, ev.title, ev.starts_at, ev.ends_at, TRUE AS own FROM events ev
WHERE ev.owner_user_id = sqlc.arg(owner_id) AND ev.tenant_id = sqlc.arg(tenant_id) AND ev.deleted_at IS NULL
  AND ev.kind = '' AND ev.starts_at < sqlc.arg(to_ts) AND ev.ends_at > sqlc.arg(from_ts)
UNION ALL
SELECT e.id, e.title, e.starts_at, e.ends_at, FALSE AS own FROM events e
JOIN event_invites i ON i.event_id = e.id
WHERE i.user_id = sqlc.arg(invitee_id) AND e.tenant_id = sqlc.arg(tenant_id) AND e.deleted_at IS NULL
  AND e.kind = '' AND i.status <> 'declined' AND e.starts_at < sqlc.arg(to_ts) AND e.ends_at > sqlc.arg(from_ts)
ORDER BY 3;

-- name: ListRecordingsPage :many
SELECT *, COUNT(*) OVER() AS total FROM recordings WHERE tenant_id = $1
ORDER BY started_at DESC LIMIT $2 OFFSET $3;

-- name: GetMailUserByEmailInTenant :one
-- Tenant-scoped variant of GetMailUserByEmail: for paths where the target MUST be in the same
-- tenant (assistant: invite/share), without relying on a post-check in the caller.
SELECT * FROM mail_users WHERE email = $1 AND tenant_id = $2;

-- name: GetFolderInTenant :one
SELECT * FROM storage_folders WHERE id = $1 AND tenant_id = $2;

-- name: NextOutboxDue :one
-- The nearest send_at still pending — the worker sleeps exactly until then (no polling). NULL
-- if there is nothing queued.
SELECT MIN(send_at)::timestamptz AS next_at FROM outbox_emails WHERE status = 'pending';

-- name: NotifyOutbox :exec
-- Wakes up the send worker: the API calls this when queuing an email (undo-send/scheduled).
SELECT pg_notify('outbox_new', '');

-- ---------- Per-member permission exceptions (granular RBAC) ----------

-- name: ListMemberPermissions :many
-- One person's exceptions. Filters by tenant: a user_id from another tenant returns nothing.
SELECT permission, allow FROM member_permissions
WHERE tenant_id = @tenant_id AND user_id = @user_id
ORDER BY permission;

-- name: DeleteMemberPermissions :exec
DELETE FROM member_permissions WHERE tenant_id = @tenant_id AND user_id = @user_id;

-- name: InsertMemberPermission :exec
INSERT INTO member_permissions (tenant_id, user_id, permission, allow, created_by)
VALUES (@tenant_id, @user_id, @permission, @allow, @created_by)
ON CONFLICT (user_id, permission) DO UPDATE SET allow = EXCLUDED.allow, created_by = EXCLUDED.created_by;

-- ---------- Disabled assistant tools (fine-grained governance) ----------

-- name: ListDisabledAssistantTools :many
SELECT tool FROM assistant_tool_settings WHERE tenant_id = @tenant_id AND enabled = false ORDER BY tool;

-- name: SetAssistantToolEnabled :exec
INSERT INTO assistant_tool_settings (tenant_id, tool, enabled)
VALUES (@tenant_id, @tool, @enabled)
ON CONFLICT (tenant_id, tool) DO UPDATE SET enabled = EXCLUDED.enabled;

-- ---------- Email aliases ----------

-- name: CreateEmailAlias :one
INSERT INTO email_aliases (tenant_id, domain_id, mail_user_id, address)
VALUES (@tenant_id, @domain_id, @mail_user_id, @address)
RETURNING *;

-- name: ListEmailAliases :many
-- Tenant's aliases with their owner, for the admin panel. The owner's name comes along to avoid an N+1 in the UI.
SELECT a.id, a.address, a.mail_user_id, a.domain_id, a.created_at,
       u.email AS owner_email, u.name AS owner_name, d.domain AS domain
FROM email_aliases a
JOIN mail_users u ON u.id = a.mail_user_id
JOIN domains d ON d.id = a.domain_id
WHERE a.tenant_id = @tenant_id
ORDER BY a.address;

-- name: ListAliasesForUser :many
-- One person's extra addresses: this is what the composer shows in "From:".
SELECT address FROM email_aliases WHERE tenant_id = @tenant_id AND mail_user_id = @user_id ORDER BY address;

-- name: ListAliasesForStalwart :many
-- One person's aliases in the format Stalwart wants: local part + the domain's id THERE (not
-- ours). See DIVERGENCES §D4b — the alias is an object {name, domainId}, not a text address.
SELECT split_part(a.address, '@', 1) AS local, COALESCE(d.stalwart_id, '') AS stalwart_domain_id
FROM email_aliases a
JOIN domains d ON d.id = a.domain_id
WHERE a.tenant_id = @tenant_id AND a.mail_user_id = @user_id
ORDER BY a.address;

-- name: GetEmailAlias :one
SELECT * FROM email_aliases WHERE id = @id AND tenant_id = @tenant_id;

-- name: DeleteEmailAlias :exec
DELETE FROM email_aliases WHERE id = @id AND tenant_id = @tenant_id;

-- name: CountEmailAliases :one
SELECT count(*) FROM email_aliases WHERE tenant_id = @tenant_id;
