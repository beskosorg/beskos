package billing

const DefaultPlanID = "self_hosted"

// The self-hosted catalog has no commercial seat, storage, or sending limits.
var plans = []Plan{
	{ID: DefaultPlanID, Name: "Community", Recording: "none", OnlyOfficeEdit: true,
		WikiPublicLinks: true, IsActive: true},
}
