package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ThomasCrouzet/icloud-mcp/internal/contacts"
)

func callListContacts(t *testing.T, deps ContactsDeps, args map[string]any) contactsListResponse {
	t.Helper()
	result, err := listContactsHandler(deps)(context.Background(), contactsRequest(args))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if result.IsError {
		t.Fatalf("list_contacts failed: %s", resultText(t, result))
	}
	var response contactsListResponse
	if err := json.Unmarshal([]byte(resultText(t, result)), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return response
}

// TestListContactsWalksEveryRowInOrder pages through the synthetic three-book
// CardDAV fixture with offset/nextOffset and expects every row exactly once,
// in the search_contacts order.
func TestListContactsWalksEveryRowInOrder(t *testing.T) {
	const limit = 250
	client, _ := newPaginationClient(&paginationCounters{})
	expected := paginationExpectedRows(t, client)
	deps := paginationDeps(t, client, nil)

	var rows []contacts.ContactSummary
	offset, pages := 0, 0
	for {
		page := callListContacts(t, deps, map[string]any{"limit": limit, "offset": offset})
		pages++
		if page.Total != len(expected) || page.Offset != offset || page.Count != len(page.Contacts) || page.ScanLimitReached {
			t.Fatalf("page %d header = total %d offset %d count %d scan %v", pages, page.Total, page.Offset, page.Count, page.ScanLimitReached)
		}
		rows = append(rows, page.Contacts...)
		if !page.HasMore {
			if page.NextOffset != nil {
				t.Fatal("last page advertises nextOffset")
			}
			break
		}
		if page.NextOffset == nil || *page.NextOffset != offset+page.Count {
			t.Fatalf("nextOffset = %v, want %d", page.NextOffset, offset+page.Count)
		}
		offset = *page.NextOffset
	}
	if want := (len(expected) + limit - 1) / limit; pages != want {
		t.Fatalf("pages = %d, want %d", pages, want)
	}
	if len(rows) != len(expected) {
		t.Fatalf("rows = %d, want %d", len(rows), len(expected))
	}
	for index := range rows {
		if paginationRowKey(rows[index]) != paginationRowKey(expected[index]) {
			t.Fatalf("row %d = %q, want %q", index, paginationRowKey(rows[index]), paginationRowKey(expected[index]))
		}
		if rows[index].Organization != "Pagination Org" || len(rows[index].Emails) != 1 || len(rows[index].Phones) != 1 {
			t.Fatalf("row %d lost summary fields: %#v", index, rows[index])
		}
	}
}

func TestListContactsScansWithoutFiltersAndKeepsPhoneLabels(t *testing.T) {
	fake := &fakeContactsService{searchResult: contacts.SearchResult{Contacts: []contacts.ContactSummary{
		{AddressBook: contactsTestBook, UID: "a", DisplayName: "Alpha Synthetic", Phones: []contacts.TypedValue{{Type: "CELL", Value: "+41 79 000 00 01"}}},
		{AddressBook: contactsTestBook, UID: "b", DisplayName: "Beta Synthetic", Organization: "Example Org"},
	}}}
	page := callListContacts(t, contactsTestDeps(fake, nil), map[string]any{"address_book": contactsTestBook})
	opts := fake.snapshot().lastSearch
	if opts.Query != "" || opts.Email != "" || opts.Phone != "" || opts.IncludeGroups || !opts.PageAll || opts.AddressBook != contactsTestBook {
		t.Fatalf("search options = %#v", opts)
	}
	if page.Total != 2 || page.Count != 2 || page.HasMore || page.NextOffset != nil {
		t.Fatalf("page = %#v", page)
	}
	if got := page.Contacts[0].Phones; len(got) != 1 || got[0].Type != "CELL" {
		t.Fatalf("phone label lost: %#v", got)
	}
}

func TestListContactsOffsetPastEndAndScanLimit(t *testing.T) {
	fake := &fakeContactsService{searchResult: contacts.SearchResult{
		ScanLimitReached: true,
		Contacts: []contacts.ContactSummary{
			{AddressBook: contactsTestBook, UID: "a", DisplayName: "Alpha"},
			{AddressBook: contactsTestBook, UID: "b", DisplayName: "Beta"},
			{AddressBook: contactsTestBook, UID: "c", DisplayName: "Gamma"},
		},
	}}
	deps := contactsTestDeps(fake, nil)

	page := callListContacts(t, deps, map[string]any{"limit": 2, "offset": 1})
	if page.Total != 3 || page.Count != 2 || page.HasMore || !page.ScanLimitReached || page.Contacts[0].UID != "b" {
		t.Fatalf("page = %#v", page)
	}
	page = callListContacts(t, deps, map[string]any{"offset": 10})
	if page.Total != 3 || page.Offset != 3 || page.Count != 0 || page.HasMore || page.Contacts == nil {
		t.Fatalf("past-end page = %#v", page)
	}
}

// TestListContactsShrinksPageToByteBudget fills summaries close to their field
// bounds, so a 500-row page cannot fit 256 KiB and must continue at nextOffset.
func TestListContactsShrinksPageToByteBudget(t *testing.T) {
	var rows []contacts.ContactSummary
	for index := range 600 {
		rows = append(rows, contacts.ContactSummary{
			AddressBook:  contactsTestBook,
			UID:          fmt.Sprintf("u-%04d", index),
			DisplayName:  fmt.Sprintf("Person %04d", index),
			Organization: strings.Repeat("o", 400),
		})
	}
	fake := &fakeContactsService{searchResult: contacts.SearchResult{Contacts: rows}}
	page := callListContacts(t, contactsTestDeps(fake, nil), map[string]any{"limit": 500})
	if page.Count == 0 || page.Count >= 500 || !page.HasMore || page.NextOffset == nil || *page.NextOffset != page.Count {
		t.Fatalf("page count %d hasMore %v next %v", page.Count, page.HasMore, page.NextOffset)
	}
}

func TestListContactsRejectsInvalidArgumentsBeforeService(t *testing.T) {
	for _, args := range []map[string]any{
		{"limit": 0},
		{"limit": 501},
		{"offset": -1},
		{"offset": contactsMaxListOffset + 1},
		{"address_book": "Contacts"},
		{"query": "x"},
	} {
		fake := &fakeContactsService{}
		result, err := listContactsHandler(contactsTestDeps(fake, nil))(context.Background(), contactsRequest(args))
		if err != nil {
			t.Fatalf("%v: handler error = %v", args, err)
		}
		payload := contactsErrorPayloadFromResult(t, result)
		if payload.Code != "validation" {
			t.Errorf("%v: code = %q", args, payload.Code)
		}
		if fake.snapshot().searchCalls != 0 {
			t.Errorf("%v: service was called", args)
		}
	}
}
