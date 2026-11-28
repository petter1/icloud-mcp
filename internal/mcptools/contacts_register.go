package mcptools

import (
	"github.com/mark3labs/mcp-go/server"
)

// RegisterContacts registers the Contacts read tools and the independently
// gated write and delete tools. It returns the names actually registered.
func RegisterContacts(s *server.MCPServer, deps ContactsDeps, allowRead, allowWrite, allowDelete bool) []string {
	if deps.Redactor == nil {
		// An empty redactor redacts nothing useful; production wiring must pass
		// a secret-aware redactor (RegisterUnified already panics on nil).
		panic("mcptools: Contacts tools require a non-nil redactor")
	}
	var names []string
	if allowRead {
		s.AddTool(newListAddressBooksTool(), listAddressBooksHandler(deps))
		s.AddTool(newSearchContactsTool(), searchContactsHandler(deps))
		s.AddTool(newListContactsTool(), listContactsHandler(deps))
		s.AddTool(newGetContactTool(), getContactHandler(deps))
		s.AddTool(newListGroupsTool(), listGroupsHandler(deps))
		s.AddTool(newListGroupMembersTool(), listGroupMembersHandler(deps))
		names = append(names, contactsReadToolNames()...)
	}
	if allowWrite {
		s.AddTool(newCreateContactTool(), createContactHandler(deps))
		s.AddTool(newUpdateContactTool(), updateContactHandler(deps))
		// Group writes edit only group vCards, and normalize_phones only TEL
		// values, so they share the create/update gate; no contact is deleted.
		s.AddTool(newCreateGroupTool(), createGroupHandler(deps))
		s.AddTool(newRenameGroupTool(), renameGroupHandler(deps))
		s.AddTool(newDeleteGroupTool(), deleteGroupHandler(deps))
		s.AddTool(newGroupMembershipTool(true), groupMembershipHandler(deps, true))
		s.AddTool(newGroupMembershipTool(false), groupMembershipHandler(deps, false))
		s.AddTool(newNormalizePhonesTool(), normalizePhonesHandler(deps))
		names = append(names, contactsWriteToolNames()...)
	}
	if allowDelete {
		s.AddTool(newDeleteContactTool(), deleteContactHandler(deps))
		names = append(names, "delete_contact")
	}
	return names
}
