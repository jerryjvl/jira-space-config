package instructions

// Where each section is done in Jira Cloud. Checked against Atlassian's
// support documentation on 2026-10-15; Atlassian renames menus often (issues
// became work items, projects became spaces), so the document says when they
// were checked.
//
// Sources:
//   - https://support.atlassian.com/jira-cloud-administration/docs/create-a-custom-field/
//   - https://support.atlassian.com/jira-cloud-administration/docs/create-or-edit-a-field-scheme/
//   - https://support.atlassian.com/jira-cloud-administration/docs/associate-a-space-with-a-field-scheme/
//   - https://support.atlassian.com/jira-cloud-administration/docs/configure-advanced-field-options-using-contexts/
//   - https://support.atlassian.com/jira-cloud-administration/docs/add-statuses-to-a-jira-workflow/
//   - https://support.atlassian.com/jira-cloud-administration/docs/manage-priorities/
//   - https://support.atlassian.com/jira-cloud-administration/docs/create-a-new-permission-scheme/
//   - https://support.atlassian.com/jira-cloud-administration/docs/create-a-new-work-item-security-scheme-and-security-levels/
//   - https://support.atlassian.com/cloud-automation/docs/import-and-export-jira-automation-rules/
//   - https://support.atlassian.com/jira-software-cloud/docs/create-a-board-based-on-filters/
//   - https://support.atlassian.com/jira-cloud-administration/docs/manage-versions/
//   - https://support.atlassian.com/jira/kb/grant-access-to-single-jira-project-that-uses-a-shared-permission-scheme/
const checked = "October 2026"

// Menu paths. "Settings" is the gear in the top navigation bar; "Space
// settings" is under More actions (…) next to the space's name in the sidebar.
const (
	pathSpace        = "**Spaces › Create space**, then a company-managed template of the type below"
	pathRoles        = "**Settings › System › Security › Project roles**: fill in the form at the bottom of the page"
	pathGroups       = "**admin.atlassian.com › Directory › Groups › Create group** (Atlassian Administration)"
	pathStatuses     = "**Settings › Work items › Statuses › Add status**"
	pathResolutions  = "**Settings › Work items › Resolutions**: fill in the form below the list"
	pathPriorities   = "**Settings › Work items › Priorities**: fill in the form below the list"
	pathLinkTypes    = "**Settings › Work items › Work item linking**: fill in the form below the list"
	pathIssueTypes   = "**Settings › Work items › Work item types › Add work item type**"
	pathFields       = "**Settings › Work items › Fields › Create new field**"
	pathContexts     = "**Settings › Work items › Fields**, then the field's **More actions › Contexts and default value**"
	pathScreens      = "**Settings › Work items › Screens › Add screen**"
	pathWorkflows    = "**Settings › Work items › Workflows › Create workflow**"
	pathITScheme     = "**Settings › Work items › Work item type schemes › Add work item type scheme**"
	pathScreenScheme = "**Settings › Work items › Screen schemes › Add screen scheme**, then **Work item type screen schemes › Add work item type screen scheme**"
	pathFieldScheme  = "**Settings › Work items › Field schemes**, create the scheme, then **Add fields**"
	pathFieldConfig  = "**Settings › Work items › Fields › Field configurations › Add field configuration**, then **Field configuration schemes**"
	pathWFScheme     = "**Settings › Work items › Workflow schemes › Add workflow scheme**"
	pathPermissions  = "**Settings › Work items › Permission schemes › Add permission scheme**, then **Permissions** to grant"
	pathNotify       = "**Settings › Work items › Notification schemes › Add notification scheme**"
	pathSecurity     = "**Settings › Work items › Work item security schemes › Add work item security scheme**, then **Security levels**"
	pathPrioScheme   = "**Settings › Work items › Priority schemes › Add priority scheme**"
	pathDetails      = "**Space settings › Details**"
	pathComponents   = "**Space settings › Components › Create component**"
	pathVersions     = "**Space settings › Versions › Create version** (the page may be called Releases)"
	pathRoleMembers  = "**Space settings › People › Add people**"
	pathSwitches     = "**Space settings**, then the page named in each row, **Actions › Use a different scheme**"
	pathFilters      = "**Filters › Search work items**: enter the JQL, **Save filter**, then **Details › Edit permissions**"
	pathBoards       = "**Search › Go to all: Boards › Create board**, choose Scrum or Kanban, then **Board from an existing Saved Filter**, with the space as its location"
	pathAutomation   = "**Space settings › Automation › Create rule**"
	pathImport       = "**Settings › System › Automation flows › More actions › Import flows**"
	pathLayouts      = "**Space settings › Work items › Layout**, then the work type"
)
