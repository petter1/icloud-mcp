package mcptools

import (
	"context"

	"github.com/ThomasCrouzet/icloud-mcp/internal/contacts"
)

// groupServiceStub completes contacts.Service for test doubles that do not
// exercise groups or phone normalization.
type groupServiceStub struct{}

func errGroupStub() error {
	return &contacts.Error{Code: contacts.CodeInternalError, Message: "group operation not stubbed"}
}

func (groupServiceStub) ListGroups(context.Context, string) (contacts.GroupList, error) {
	return contacts.GroupList{}, errGroupStub()
}

func (groupServiceStub) ListGroupMembers(context.Context, string, string) (contacts.GroupMembers, error) {
	return contacts.GroupMembers{}, errGroupStub()
}

func (groupServiceStub) CreateGroup(context.Context, string, string) (contacts.Group, error) {
	return contacts.Group{}, errGroupStub()
}

func (groupServiceStub) RenameGroup(context.Context, *contacts.RenameGroupInput) (contacts.RenameGroupResult, error) {
	return contacts.RenameGroupResult{}, errGroupStub()
}

func (groupServiceStub) DeleteGroup(context.Context, *contacts.DeleteGroupInput) (contacts.DeleteGroupResult, error) {
	return contacts.DeleteGroupResult{}, errGroupStub()
}

func (groupServiceStub) ChangeGroupMembers(context.Context, *contacts.GroupMembershipInput) (contacts.GroupMembershipResult, error) {
	return contacts.GroupMembershipResult{}, errGroupStub()
}

func (groupServiceStub) NormalizePhones(context.Context, *contacts.NormalizePhonesInput) (contacts.NormalizePhonesResult, error) {
	return contacts.NormalizePhonesResult{}, errGroupStub()
}
