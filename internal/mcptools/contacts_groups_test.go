package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ThomasCrouzet/icloud-mcp/internal/contacts"
)

type recordingGroupService struct {
	fakeContactsService
	normalize  []contacts.NormalizePhonesInput
	membership []contacts.GroupMembershipInput
}

func (s *recordingGroupService) NormalizePhones(_ context.Context, input *contacts.NormalizePhonesInput) (contacts.NormalizePhonesResult, error) {
	s.normalize = append(s.normalize, *input)
	return contacts.NormalizePhonesResult{Applied: input.Apply, CountryCode: "+" + input.CountryCode, Planned: []contacts.PlannedPhoneChanges{}}, nil
}

func (s *recordingGroupService) ChangeGroupMembers(_ context.Context, input *contacts.GroupMembershipInput) (contacts.GroupMembershipResult, error) {
	s.membership = append(s.membership, *input)
	return contacts.GroupMembershipResult{
		Group:   contacts.Group{AddressBook: input.AddressBook, UID: input.UID, Name: "Synthetic"},
		Changed: []string{"a"}, Unchanged: []string{"b"}, NotFound: nil,
	}, nil
}

func TestNormalizePhonesPreviewIsNotAuditedAndApplyIs(t *testing.T) {
	service := &recordingGroupService{}
	var audit bytes.Buffer
	handler := normalizePhonesHandler(contactsTestDeps(service, &audit))

	preview, err := handler(context.Background(), contactsRequest(map[string]any{"address_book": contactsTestBook, "country_code": "+49"}))
	if err != nil || preview.IsError {
		t.Fatalf("preview: %v %s", err, resultText(t, preview))
	}
	if audit.Len() != 0 || len(service.normalize) != 1 || service.normalize[0].Apply || service.normalize[0].CountryCode != "49" {
		t.Fatalf("preview audit=%q input=%#v", audit.String(), service.normalize)
	}
	applied, err := handler(context.Background(), contactsRequest(map[string]any{"address_book": contactsTestBook, "apply": true}))
	if err != nil || applied.IsError || !service.normalize[1].Apply {
		t.Fatalf("apply: %v %s", err, resultText(t, applied))
	}
	if !strings.Contains(audit.String(), "normalize_phones") {
		t.Fatalf("apply was not audited: %q", audit.String())
	}
	for _, args := range []map[string]any{
		{"address_book": contactsTestBook, "group_uid": "g", "contact_uids": []any{"c"}},
		{"address_book": contactsTestBook, "contact_uids": []any{}},
		{"address_book": contactsTestBook, "country_code": "4444"},
	} {
		res, _ := handler(context.Background(), contactsRequest(args))
		if contactsErrorPayloadFromResult(t, res).Code != "validation" {
			t.Fatalf("args %v were accepted", args)
		}
	}
	if len(service.normalize) != 2 {
		t.Fatalf("invalid input reached the service: %#v", service.normalize)
	}
}

func TestGroupMembershipResponseNames(t *testing.T) {
	service := &recordingGroupService{}
	for _, add := range []bool{true, false} {
		handler := groupMembershipHandler(contactsTestDeps(service, nil), add)
		res, err := handler(context.Background(), contactsRequest(map[string]any{"address_book": contactsTestBook, "uid": "g", "contact_uids": []any{"a", "b"}}))
		if err != nil || res.IsError {
			t.Fatalf("membership: %v %s", err, resultText(t, res))
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(resultText(t, res)), &payload); err != nil {
			t.Fatal(err)
		}
		changed, unchanged := "removed", "notMembers"
		if add {
			changed, unchanged = "added", "alreadyMembers"
		}
		if payload[changed] == nil || payload[unchanged] == nil || payload["notFound"] == nil {
			t.Fatalf("add=%v payload = %v", add, payload)
		}
	}
	if len(service.membership) != 2 || !service.membership[0].Add || service.membership[1].Add {
		t.Fatalf("membership inputs = %#v", service.membership)
	}
}
