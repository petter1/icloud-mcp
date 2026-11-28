package contacts

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
)

// bookFixture is a CardDAV fake for one address book with several vCards. PUT
// and DELETE honor strong If-Match / If-None-Match like iCloud.
type bookFixture struct {
	mu        sync.Mutex
	cards     map[string]string // href -> vCard
	etags     map[string]string // href -> ETag
	version   int
	conflicts map[string]bool // hrefs whose next conditional write fails with 412
	records   []requestRecord
}

func newBookFixture(cards map[string]string) *bookFixture {
	fixture := &bookFixture{cards: map[string]string{}, etags: map[string]string{}, conflicts: map[string]bool{}}
	for name, card := range cards {
		href := "/home/book/" + name
		fixture.cards[href] = card
		fixture.etags[href] = `"e-` + name + `"`
	}
	return fixture
}

func (fixture *bookFixture) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.records = append(fixture.records, requestRecord{method: req.Method, path: req.URL.Path, header: req.Header.Clone(), body: string(body)})
	path := req.URL.Path
	switch {
	case req.Method == "PROPFIND" && path == "/":
		return httpResponse(req, http.StatusMultiStatus, principalXML("/principal/"), nil), nil
	case req.Method == "PROPFIND" && path == "/principal/":
		return httpResponse(req, http.StatusMultiStatus, homesXML("/home/"), nil), nil
	case req.Method == "PROPFIND" && path == "/home/":
		return httpResponse(req, http.StatusMultiStatus, booksXML("/home/book/"), nil), nil
	case req.Method == "REPORT":
		uid := ""
		if strings.Contains(string(body), `name="UID"`) {
			start := strings.Index(string(body), `match-type="equals">`) + len(`match-type="equals">`)
			end := strings.Index(string(body), `</C:text-match>`)
			uid = html.UnescapeString(string(body)[start:end])
		}
		hrefs := make([]string, 0, len(fixture.cards))
		for href := range fixture.cards {
			hrefs = append(hrefs, href)
		}
		sort.Strings(hrefs)
		var rows strings.Builder
		rows.WriteString(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">`)
		for _, href := range hrefs {
			card := fixture.cards[href]
			if uid != "" && !strings.Contains(card, "\r\nUID:"+uid+"\r\n") {
				continue
			}
			rows.WriteString(`<D:response><D:href>` + xmlEscape(href) + `</D:href><D:propstat><D:prop><D:getetag>` + xmlEscape(fixture.etags[href]) +
				`</D:getetag><C:address-data>` + xmlEscape(card) + `</C:address-data></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`)
		}
		rows.WriteString(`</D:multistatus>`)
		return httpResponse(req, http.StatusMultiStatus, rows.String(), nil), nil
	case req.Method == http.MethodGet:
		card, ok := fixture.cards[path]
		if !ok {
			return httpResponse(req, http.StatusNotFound, "", nil), nil
		}
		return httpResponse(req, http.StatusOK, card, map[string]string{"Content-Type": "text/vcard", "ETag": fixture.etags[path]}), nil
	case req.Method == http.MethodPut:
		_, exists := fixture.cards[path]
		switch {
		case req.Header.Get("If-None-Match") == "*" && exists,
			req.Header.Get("If-None-Match") != "*" && (!exists || req.Header.Get("If-Match") != fixture.etags[path]),
			fixture.conflicts[path]:
			return httpResponse(req, http.StatusPreconditionFailed, "", nil), nil
		}
		fixture.version++
		fixture.cards[path] = string(body)
		fixture.etags[path] = fmt.Sprintf(`"v%d"`, fixture.version)
		return httpResponse(req, http.StatusNoContent, "", nil), nil
	case req.Method == http.MethodDelete:
		if _, exists := fixture.cards[path]; !exists {
			return httpResponse(req, http.StatusNotFound, "", nil), nil
		}
		if req.Header.Get("If-Match") != fixture.etags[path] {
			return httpResponse(req, http.StatusPreconditionFailed, "", nil), nil
		}
		delete(fixture.cards, path)
		delete(fixture.etags, path)
		return httpResponse(req, http.StatusNoContent, "", nil), nil
	}
	return httpResponse(req, http.StatusNotFound, "", nil), nil
}

func (fixture *bookFixture) writes() []requestRecord {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var out []requestRecord
	for _, record := range fixture.records {
		if record.method == http.MethodPut || record.method == http.MethodDelete {
			out = append(out, record)
		}
	}
	return out
}

func (fixture *bookFixture) card(name string) string {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.cards["/home/book/"+name]
}

// appleGroup builds a group vCard the way iCloud stores it: a plain N value.
func appleGroup(uid, name string, members ...string) string {
	lines := []string{"BEGIN:VCARD", "VERSION:3.0", "PRODID:-//Apple Inc.//Synthetic//EN", "N:" + name, "FN:" + name, "X-ADDRESSBOOKSERVER-KIND:group"}
	for _, member := range members {
		lines = append(lines, "X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:"+member)
	}
	lines = append(lines, "REV:2026-01-01T00:00:00Z", "UID:"+uid, "END:VCARD")
	return strings.Join(lines, "\r\n") + "\r\n"
}

func groupFixtureClient(t *testing.T, cards map[string]string) (*Client, *bookFixture, string) {
	t.Helper()
	fixture := newBookFixture(cards)
	client := NewClient(fixture, "https://contacts.icloud.com/", allowContactsHost)
	return client, fixture, discoveredBook(t, client).Identifier
}

func standardGroupCards() map[string]string {
	return map[string]string{
		"ada.vcf":     v3Card("ada", "Ada Example", "TEL;TYPE=CELL:079 000 00 01"),
		"bob.vcf":     v3Card("bob", "Bob Example"),
		"friends.vcf": appleGroup("friends", "Friends", "ada", "gone"),
		"work.vcf":    appleGroup("work", "Work"),
	}
}

func TestListGroupsAndMembers(t *testing.T) {
	client, _, book := groupFixtureClient(t, standardGroupCards())
	list, err := client.ListGroups(context.Background(), book)
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if len(list.Groups) != 2 || list.Groups[0].Name != "Friends" || list.Groups[0].MemberCount != 2 || list.Groups[1].UID != "work" || list.Groups[0].ETag != `"e-friends.vcf"` {
		t.Fatalf("groups = %#v", list.Groups)
	}
	members, err := client.ListGroupMembers(context.Background(), book, "friends")
	if err != nil {
		t.Fatalf("ListGroupMembers() error = %v", err)
	}
	if len(members.Members) != 1 || members.Members[0].UID != "ada" || members.UnresolvedMembers != 1 {
		t.Fatalf("members = %#v", members)
	}
	if _, err := client.ListGroupMembers(context.Background(), book, "ada"); AsError(err) == nil || AsError(err).Code != CodeNotFound {
		t.Fatalf("contact UID as group: %v", err)
	}
	// Apple group vCards stay hidden from contact searches.
	result, err := client.SearchContacts(context.Background(), SearchOptions{AddressBook: book})
	if err != nil || len(result.Contacts) != 2 {
		t.Fatalf("SearchContacts() = %#v, %v", result.Contacts, err)
	}
}

func TestCreateGroupWritesAppleGroupCard(t *testing.T) {
	client, fixture, book := groupFixtureClient(t, standardGroupCards())
	if _, err := client.CreateGroup(context.Background(), book, " Friends "); AsError(err) == nil || AsError(err).Code != CodeConflict {
		t.Fatalf("duplicate name: %v", err)
	}
	group, err := client.CreateGroup(context.Background(), book, "Leads; 2026")
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	writes := fixture.writes()
	if len(writes) != 1 || writes[0].header.Get("If-None-Match") != "*" || writes[0].path != "/home/book/"+group.UID+".vcf" {
		t.Fatalf("create writes = %#v", writes)
	}
	for _, line := range []string{"X-ADDRESSBOOKSERVER-KIND:group\r\n", "FN:Leads\\; 2026\r\n", "N:Leads\\; 2026;;;;\r\n", "UID:" + group.UID + "\r\n"} {
		if !strings.Contains(writes[0].body, line) {
			t.Errorf("create body lacks %q:\n%s", line, writes[0].body)
		}
	}
	if group.Name != "Leads; 2026" || group.MemberCount != 0 || group.ETag == "" {
		t.Fatalf("group = %#v", group)
	}
}

func TestRenameGroupKeepsEverythingElse(t *testing.T) {
	client, fixture, book := groupFixtureClient(t, standardGroupCards())
	before := fixture.card("friends.vcf")
	result, err := client.RenameGroup(context.Background(), &RenameGroupInput{AddressBook: book, UID: "friends", Name: "Close friends"})
	if err != nil {
		t.Fatalf("RenameGroup() error = %v", err)
	}
	writes := fixture.writes()
	if len(writes) != 1 || writes[0].header.Get("If-Match") != `"e-friends.vcf"` {
		t.Fatalf("rename writes = %#v", writes)
	}
	want := strings.Replace(strings.Replace(before, "N:Friends\r\nFN:Friends\r\n", "", 1), "END:VCARD", "N:Close friends;;;;\r\nFN:Close friends\r\nEND:VCARD", 1)
	if writes[0].body != want {
		t.Fatalf("rename body\ngot:\n%s\nwant:\n%s", writes[0].body, want)
	}
	if result.PreviousName != "Friends" || result.Name != "Close friends" || result.MemberCount != 2 {
		t.Fatalf("rename result = %#v", result)
	}
	if _, err := client.RenameGroup(context.Background(), &RenameGroupInput{AddressBook: book, UID: "ada", Name: "X"}); AsError(err) == nil || AsError(err).Code != CodeValidation {
		t.Fatalf("rename of a contact: %v", err)
	}
	if _, err := client.RenameGroup(context.Background(), &RenameGroupInput{AddressBook: book, UID: "work", Name: "Y", ETag: `"stale"`}); AsError(err) == nil || AsError(err).Code != CodeConcurrentModification {
		t.Fatalf("stale rename: %v", err)
	}
}

func TestDeleteGroupNeverTouchesContacts(t *testing.T) {
	client, fixture, book := groupFixtureClient(t, standardGroupCards())
	if _, err := client.DeleteGroup(context.Background(), &DeleteGroupInput{AddressBook: book, UID: "friends"}); AsError(err) == nil || AsError(err).Code != CodeValidation {
		t.Fatalf("delete with members and no force: %v", err)
	}
	if _, err := client.DeleteGroup(context.Background(), &DeleteGroupInput{AddressBook: book, UID: "ada", Force: true}); AsError(err) == nil || AsError(err).Code != CodeValidation {
		t.Fatalf("delete of a contact UID: %v", err)
	}
	if len(fixture.writes()) != 0 {
		t.Fatalf("refused deletes wrote: %#v", fixture.writes())
	}
	result, err := client.DeleteGroup(context.Background(), &DeleteGroupInput{AddressBook: book, UID: "friends", Force: true})
	if err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}
	writes := fixture.writes()
	if len(writes) != 1 || writes[0].method != http.MethodDelete || writes[0].path != "/home/book/friends.vcf" || writes[0].header.Get("If-Match") != `"e-friends.vcf"` {
		t.Fatalf("delete writes = %#v", writes)
	}
	if !result.Deleted || strings.Join(result.FormerMemberUIDs, ",") != "ada,gone" || fixture.card("ada.vcf") == "" {
		t.Fatalf("delete result = %#v", result)
	}
}

func TestChangeGroupMembersEditsOnlyTheGroup(t *testing.T) {
	client, fixture, book := groupFixtureClient(t, standardGroupCards())
	added, err := client.ChangeGroupMembers(context.Background(), &GroupMembershipInput{AddressBook: book, UID: "friends", ContactUIDs: []string{"bob", "ada", "nobody", "bob"}, Add: true})
	if err != nil {
		t.Fatalf("add error = %v", err)
	}
	if strings.Join(added.Changed, ",") != "bob" || strings.Join(added.Unchanged, ",") != "ada" || strings.Join(added.NotFound, ",") != "nobody" || added.Group.MemberCount != 3 {
		t.Fatalf("add result = %#v", added)
	}
	if !strings.Contains(fixture.card("friends.vcf"), "X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:bob\r\nEND:VCARD") {
		t.Fatalf("member line missing:\n%s", fixture.card("friends.vcf"))
	}
	removed, err := client.ChangeGroupMembers(context.Background(), &GroupMembershipInput{AddressBook: book, UID: "friends", ContactUIDs: []string{"ada", "gone", "bob2"}})
	if err != nil {
		t.Fatalf("remove error = %v", err)
	}
	if strings.Join(removed.Changed, ",") != "ada,gone" || strings.Join(removed.Unchanged, ",") != "bob2" || removed.Group.MemberCount != 1 {
		t.Fatalf("remove result = %#v", removed)
	}
	for _, write := range fixture.writes() {
		if write.path != "/home/book/friends.vcf" || write.header.Get("If-Match") == "" {
			t.Fatalf("membership wrote outside the group or unconditionally: %#v", write)
		}
	}
	// Nothing to change means no write at all.
	count := len(fixture.writes())
	if _, err := client.ChangeGroupMembers(context.Background(), &GroupMembershipInput{AddressBook: book, UID: "friends", ContactUIDs: []string{"bob"}, Add: true}); err != nil || len(fixture.writes()) != count {
		t.Fatalf("no-op add wrote: %v", err)
	}
}

func TestChangeGroupMembersReportsConflicts(t *testing.T) {
	client, fixture, book := groupFixtureClient(t, standardGroupCards())
	fixture.conflicts["/home/book/work.vcf"] = true
	if _, err := client.ChangeGroupMembers(context.Background(), &GroupMembershipInput{AddressBook: book, UID: "work", ContactUIDs: []string{"ada"}, Add: true}); AsError(err) == nil || AsError(err).Code != CodeConcurrentModification {
		t.Fatalf("conflicting add: %v", err)
	}
	if strings.Contains(fixture.card("work.vcf"), "urn:uuid:ada") {
		t.Fatal("conflicting add overwrote the group")
	}
}

func TestNormalizePhonesPreviewAndApply(t *testing.T) {
	cards := map[string]string{
		"ada.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:ada\r\nFN:Ada Example\r\nN:Example;Ada;;;\r\n" +
			"item1.TEL;type=CELL;type=pref:079 000 00 01\r\nitem1.X-ABLabel:_$!<Mobile>!$_\r\n" +
			"TEL;TYPE=WORK:+41 44 000 00 00\r\nNOTE:keep me\r\nEND:VCARD\r\n",
		"bob.vcf":     v3Card("bob", "Bob Example", "TEL:0049 176 00000000"),
		"cleo.vcf":    v3Card("cleo", "Cleo Example", "TEL:117"),
		"friends.vcf": appleGroup("friends", "Friends", "bob"),
	}
	client, fixture, book := groupFixtureClient(t, cards)
	adaBefore := fixture.card("ada.vcf")

	preview, err := client.NormalizePhones(context.Background(), &NormalizePhonesInput{AddressBook: book})
	if err != nil {
		t.Fatalf("preview error = %v", err)
	}
	if preview.Applied || preview.ContactsScanned != 3 || preview.ContactsChanged != 2 || len(preview.Planned) != 2 || preview.CountryCode != "+41" || len(fixture.writes()) != 0 {
		t.Fatalf("preview = %#v, writes %d", preview, len(fixture.writes()))
	}
	if got := preview.Planned[0]; got.UID != "ada" || len(got.Changes) != 1 || got.Changes[0] != (PhoneChange{From: "079 000 00 01", To: "+41 79 000 00 01"}) {
		t.Fatalf("ada plan = %#v", got)
	}
	if got := preview.Planned[1]; got.UID != "bob" || got.Changes[0].To != "+49 176 00000000" {
		t.Fatalf("bob plan = %#v", got)
	}

	group, err := client.NormalizePhones(context.Background(), &NormalizePhonesInput{AddressBook: book, GroupUID: "friends"})
	if err != nil || group.ContactsScanned != 1 || len(group.Planned) != 1 || group.Planned[0].UID != "bob" {
		t.Fatalf("group preview = %#v, %v", group, err)
	}
	picked, err := client.NormalizePhones(context.Background(), &NormalizePhonesInput{AddressBook: book, ContactUIDs: []string{"ada", "nobody"}})
	if err != nil || len(picked.Planned) != 1 || strings.Join(picked.NotFound, ",") != "nobody" {
		t.Fatalf("picked preview = %#v, %v", picked, err)
	}

	fixture.conflicts["/home/book/bob.vcf"] = true
	applied, err := client.NormalizePhones(context.Background(), &NormalizePhonesInput{AddressBook: book, Apply: true})
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !applied.Applied || applied.Saved == nil || *applied.Saved != 1 || strings.Join(applied.SavedUIDs, ",") != "ada" ||
		len(applied.Failed) != 1 || applied.Failed[0].UID != "bob" || applied.Failed[0].Code != CodeConcurrentModification {
		t.Fatalf("apply = %#v", applied)
	}
	want := strings.Replace(adaBefore, "item1.TEL;type=CELL;type=pref:079 000 00 01\r\n", "item1.TEL;type=CELL;type=pref:+41 79 000 00 01\r\n", 1)
	if got := fixture.card("ada.vcf"); got != want {
		t.Fatalf("apply changed more than the TEL value\ngot:\n%s\nwant:\n%s", got, want)
	}
	for _, write := range fixture.writes() {
		if write.method != http.MethodPut || !strings.HasPrefix(write.header.Get("If-Match"), `"e-`) {
			t.Fatalf("apply write was not conditional on the scanned ETag: %#v", write)
		}
	}
	if !strings.Contains(fixture.card("bob.vcf"), "TEL:0049 176 00000000") {
		t.Fatal("conflicting contact was overwritten")
	}
	if _, err := client.NormalizePhones(context.Background(), &NormalizePhonesInput{AddressBook: book, CountryCode: "4x"}); AsError(err) == nil || AsError(err).Code != CodeValidation {
		t.Fatalf("bad country code: %v", err)
	}
}

func TestNormalizePhonesStopsAtTheWriteRateLimit(t *testing.T) {
	cards := map[string]string{}
	for index := range 5 {
		cards[fmt.Sprintf("p%d.vcf", index)] = v3Card(fmt.Sprintf("p%d", index), fmt.Sprintf("Person %d", index), "TEL:079 000 00 0"+fmt.Sprint(index))
	}
	client, fixture, book := groupFixtureClient(t, cards)
	result, err := client.NormalizePhones(context.Background(), &NormalizePhonesInput{AddressBook: book, Apply: true})
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if *result.Saved != 3 || len(result.Failed) != 2 || result.Failed[0].Code != CodeRateLimited || result.Failed[1].Code != CodeRateLimited || len(fixture.writes()) != 3 {
		t.Fatalf("rate-limited apply = %#v, writes %d", result, len(fixture.writes()))
	}
}
