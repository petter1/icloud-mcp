package contacts

import (
	"bytes"
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/emersion/go-vcard"
)

// Apple stores a contact group as its own vCard 3.0 resource in the address
// book: X-ADDRESSBOOKSERVER-KIND:group plus one X-ADDRESSBOOKSERVER-MEMBER
// line per member, "urn:uuid:" followed by the member's UID. Group writes edit
// only that group resource; contact resources are never written or deleted by
// a group operation.
const (
	appleMemberField    = "X-ADDRESSBOOKSERVER-MEMBER"
	appleMemberPrefix   = "urn:uuid:"
	MaxGroupBatch       = 100
	MaxNormalizeChanges = 100
	DefaultCountryCode  = "41"
)

// Group is one contact group. UID identifies it within AddressBook, like a
// contact UID.
type Group struct {
	AddressBook string `json:"addressBook"`
	UID         string `json:"uid"`
	ETag        string `json:"etag,omitempty"`
	Name        string `json:"name"`
	MemberCount int    `json:"memberCount"`
}

// GroupList lists the groups of one address book, sorted by name.
type GroupList struct {
	Groups           []Group `json:"groups"`
	ScanLimitReached bool    `json:"scanLimitReached,omitempty"`
}

// GroupMembers resolves a group's member UIDs to contact summaries sorted like
// search_contacts. UnresolvedMembers counts member UIDs without a contact in
// the address book.
type GroupMembers struct {
	Group             Group            `json:"group"`
	Members           []ContactSummary `json:"members"`
	UnresolvedMembers int              `json:"unresolvedMembers,omitempty"`
	ScanLimitReached  bool             `json:"scanLimitReached,omitempty"`
}

// RenameGroupInput renames one group. ETag is an optional caller precondition.
type RenameGroupInput struct {
	AddressBook string `json:"address_book"`
	UID         string `json:"uid"`
	Name        string `json:"name"`
	ETag        string `json:"etag,omitempty"`
}

// RenameGroupResult reports the renamed group and its former name.
type RenameGroupResult struct {
	Group
	PreviousName     string `json:"previousName"`
	ResultIncomplete bool   `json:"resultIncomplete,omitempty"`
}

// DeleteGroupInput deletes only the group resource. A group with members is
// refused unless Force is set.
type DeleteGroupInput struct {
	AddressBook string `json:"address_book"`
	UID         string `json:"uid"`
	ETag        string `json:"etag,omitempty"`
	Force       bool   `json:"force,omitempty"`
}

// DeleteGroupResult lists the member UIDs the group held. The contacts stay.
type DeleteGroupResult struct {
	Deleted          bool     `json:"deleted"`
	AddressBook      string   `json:"addressBook"`
	UID              string   `json:"uid"`
	Name             string   `json:"name"`
	FormerMemberUIDs []string `json:"formerMemberUids"`
}

// GroupMembershipInput adds (Add=true) or removes contact UIDs.
type GroupMembershipInput struct {
	AddressBook string   `json:"address_book"`
	UID         string   `json:"uid"`
	ETag        string   `json:"etag,omitempty"`
	ContactUIDs []string `json:"contact_uids"`
	Add         bool     `json:"add"`
}

// GroupMembershipResult reports changed UIDs, UIDs that already had the
// requested state, and UIDs without a contact in the address book.
type GroupMembershipResult struct {
	Group            Group    `json:"group"`
	Changed          []string `json:"changed"`
	Unchanged        []string `json:"unchanged"`
	NotFound         []string `json:"notFound"`
	ResultIncomplete bool     `json:"resultIncomplete,omitempty"`
}

// NormalizePhonesInput selects the contacts whose phone numbers are rewritten
// to international form. Without Apply nothing is written.
type NormalizePhonesInput struct {
	AddressBook string   `json:"address_book"`
	GroupUID    string   `json:"group_uid,omitempty"`
	ContactUIDs []string `json:"contact_uids,omitempty"`
	CountryCode string   `json:"country_code,omitempty"`
	Apply       bool     `json:"apply,omitempty"`
}

// PhoneChange is one rewritten number.
type PhoneChange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PlannedPhoneChanges lists the changes for one contact.
type PlannedPhoneChanges struct {
	AddressBook string        `json:"addressBook"`
	UID         string        `json:"uid"`
	Name        string        `json:"name"`
	Changes     []PhoneChange `json:"changes"`
}

// FailedContact reports a contact that was not saved and why.
type FailedContact struct {
	UID   string `json:"uid"`
	Code  Code   `json:"code"`
	Error string `json:"error"`
}

// NormalizePhonesResult is the preview, and with Applied the save outcome.
type NormalizePhonesResult struct {
	Applied          bool                  `json:"applied"`
	CountryCode      string                `json:"countryCode"`
	ContactsScanned  int                   `json:"contactsScanned"`
	ContactsChanged  int                   `json:"contactsChanged"`
	Planned          []PlannedPhoneChanges `json:"planned"`
	Truncated        bool                  `json:"truncated"`
	ReadOnlySkipped  int                   `json:"readOnlySkipped,omitempty"`
	NotFound         []string              `json:"notFound,omitempty"`
	ScanLimitReached bool                  `json:"scanLimitReached,omitempty"`
	Saved            *int                  `json:"saved,omitempty"`
	SavedUIDs        []string              `json:"savedUids,omitempty"`
	Failed           []FailedContact       `json:"failed,omitempty"`
}

// scanBook reads every vCard of one address book within the 2,000-card and
// 32 MiB bounds. limited reports that the bounded scan stopped early.
func (c *Client) scanBook(ctx context.Context, book bookRecord) ([]addressObject, bool, error) {
	requestLimit := maxCardsScanned + 1
	objects, overflow, _, err := c.report(ctx, book, searchQuery(SearchOptions{}, requestLimit), requestLimit, maxReportBytes)
	if err != nil {
		return nil, false, err
	}
	limited := overflow || len(objects) > maxCardsScanned
	if len(objects) > maxCardsScanned {
		objects = objects[:maxCardsScanned]
	}
	return objects, limited, nil
}

func groupName(card vcard.Card) string {
	if name := strings.TrimSpace(card.Value(vcard.FieldFormattedName)); name != "" {
		return name
	}
	components, err := structuredComponents(card.Value(vcard.FieldName))
	if err == nil && len(components) > 0 {
		return strings.TrimSpace(components[0])
	}
	return ""
}

// groupMemberUIDs returns the member UIDs in document order without duplicates.
func groupMemberUIDs(raw *rawCard) []string {
	seen := make(map[string]bool)
	var members []string
	for _, property := range raw.properties {
		if uid, ok := memberUID(property); ok && !seen[uid] {
			seen[uid] = true
			members = append(members, uid)
		}
	}
	return members
}

func memberUID(property rawProperty) (string, bool) {
	if property.name != appleMemberField {
		return "", false
	}
	value := strings.TrimSpace(property.value)
	if len(value) >= len(appleMemberPrefix) && strings.EqualFold(value[:len(appleMemberPrefix)], appleMemberPrefix) {
		value = value[len(appleMemberPrefix):]
	}
	return value, value != ""
}

func groupFromObject(object *addressObject, addressBook string) Group {
	return Group{
		AddressBook: addressBook,
		UID:         object.card.Value(vcard.FieldUID),
		ETag:        readableETag(object.etag),
		Name:        groupName(object.card),
		MemberCount: len(groupMemberUIDs(object.raw)),
	}
}

// rebuildRawCard writes the raw card again. replace may substitute or drop
// (nil bytes) a property it handles; extra is inserted before END:VCARD. The
// result must decode as vCard 3.0 and fit the resource limit.
func rebuildRawCard(raw *rawCard, replace func(int, rawProperty) ([]byte, bool), extra []byte, serverMax int64) ([]byte, error) {
	var output bytes.Buffer
	for index, property := range raw.properties {
		if index == len(raw.properties)-1 {
			output.Write(extra)
		}
		if replace != nil && index > 1 && index < len(raw.properties)-1 {
			if replacement, handled := replace(index, property); handled {
				output.Write(replacement)
				continue
			}
		}
		output.Write(property.raw)
	}
	limit := int64(maxCardBytes)
	if serverMax > 0 && serverMax < limit {
		limit = serverMax
	}
	if int64(output.Len()) > limit {
		return nil, newError(CodePayloadTooLarge, 0, "the encoded contact exceeds its resource limit")
	}
	encoded := output.Bytes()
	if _, version, _, err := decodeRawCard(encoded); err != nil {
		return nil, err
	} else if version != "3.0" {
		return nil, validationError("only vCard 3.0 resources can be written")
	}
	return append([]byte(nil), encoded...), nil
}

// putExisting writes a changed resource with a strong If-Match precondition.
// A changed resource returns concurrent_modification; nothing is overwritten.
func (c *Client) putExisting(ctx context.Context, book bookRecord, object *addressObject, etag string, encoded []byte) error {
	header := make(http.Header)
	header.Set("Content-Type", "text/vcard; charset=utf-8")
	header.Set("If-Match", etag)
	response, err := c.doDAV(ctx, http.MethodPut, object.url, header, encoded, maxErrorBodyBytes, book.url)
	if err != nil {
		return err
	}
	if response.Status/100 != 2 {
		return classifyDAVError(response, false)
	}
	return nil
}

// writableGroup finds a group by exact UID and returns the If-Match ETag to
// use: the caller's when given, otherwise the server's.
func (c *Client) writableGroup(ctx context.Context, addressBook, uid, callerETag string) (bookRecord, *addressObject, string, error) {
	if err := validateUID(uid); err != nil {
		return bookRecord{}, nil, "", err
	}
	etag, err := normalizeCallerETag(callerETag)
	if err != nil {
		return bookRecord{}, nil, "", err
	}
	book, err := c.book(ctx, addressBook)
	if err != nil {
		return bookRecord{}, nil, "", err
	}
	object, err := c.findByUID(ctx, book, uid)
	if err != nil {
		return bookRecord{}, nil, "", err
	}
	if !cardIsGroup(object.card) {
		return bookRecord{}, nil, "", validationError("the UID belongs to a contact, not a group")
	}
	if object.version != "3.0" {
		return bookRecord{}, nil, "", validationError("vCard 4.0 groups are read-only")
	}
	serverETag, err := validateServerETag(object.etag)
	if err != nil {
		return bookRecord{}, nil, "", err
	}
	if etag == "" {
		etag = serverETag
	}
	return book, object, etag, nil
}

func validateGroupName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if err := validateSingleLine("name", trimmed, maxDisplayNameBytes, true); err != nil {
		return "", err
	}
	return trimmed, nil
}

func groupNameLines(name string) string {
	escaped := escapeTextValue(name)
	return "N:" + escaped + ";;;;\r\nFN:" + escaped + "\r\n"
}

// ListGroups lists the groups of one address book with member counts.
func (c *Client) ListGroups(ctx context.Context, addressBook string) (GroupList, error) {
	book, err := c.book(ctx, addressBook)
	if err != nil {
		return GroupList{}, err
	}
	objects, limited, err := c.scanBook(ctx, book)
	if err != nil {
		return GroupList{}, err
	}
	result := GroupList{Groups: []Group{}, ScanLimitReached: limited}
	for index := range objects {
		if cardIsGroup(objects[index].card) {
			result.Groups = append(result.Groups, groupFromObject(&objects[index], addressBook))
		}
	}
	sort.Slice(result.Groups, func(i, j int) bool {
		left, right := normalizeForSort(result.Groups[i].Name), normalizeForSort(result.Groups[j].Name)
		if left != right {
			return left < right
		}
		return result.Groups[i].UID < result.Groups[j].UID
	})
	return result, nil
}

// ListGroupMembers returns the contacts of one group.
func (c *Client) ListGroupMembers(ctx context.Context, addressBook, uid string) (GroupMembers, error) {
	if err := validateUID(uid); err != nil {
		return GroupMembers{}, err
	}
	book, err := c.book(ctx, addressBook)
	if err != nil {
		return GroupMembers{}, err
	}
	objects, limited, err := c.scanBook(ctx, book)
	if err != nil {
		return GroupMembers{}, err
	}
	var group *addressObject
	byUID := make(map[string]*addressObject, len(objects))
	for index := range objects {
		object := &objects[index]
		objectUID := object.card.Value(vcard.FieldUID)
		if cardIsGroup(object.card) {
			if objectUID == uid {
				group = object
			}
			continue
		}
		byUID[objectUID] = object
	}
	if group == nil {
		if limited {
			return GroupMembers{}, newError(CodePartialFailure, 0, "the bounded scan ended before the group was found")
		}
		return GroupMembers{}, newError(CodeNotFound, http.StatusNotFound, "group UID was not found in the address book")
	}
	result := GroupMembers{Group: groupFromObject(group, addressBook), Members: []ContactSummary{}, ScanLimitReached: limited}
	for _, member := range groupMemberUIDs(group.raw) {
		object, ok := byUID[member]
		if !ok {
			result.UnresolvedMembers++
			continue
		}
		result.Members = append(result.Members, cardSummary(newCardView(object.card, object.raw), addressBook, object.etag))
	}
	sortSummaries(result.Members)
	return result, nil
}

func sortSummaries(rows []ContactSummary) {
	sort.Slice(rows, func(i, j int) bool {
		left, right := normalizeForSort(rows[i].DisplayName), normalizeForSort(rows[j].DisplayName)
		if left != right {
			return left < right
		}
		if rows[i].UID != rows[j].UID {
			return rows[i].UID < rows[j].UID
		}
		return rows[i].AddressBook < rows[j].AddressBook
	})
}

// CreateGroup creates an empty group with a unique name.
func (c *Client) CreateGroup(ctx context.Context, addressBook, name string) (Group, error) {
	name, err := validateGroupName(name)
	if err != nil {
		return Group{}, err
	}
	book, err := c.book(ctx, addressBook)
	if err != nil {
		return Group{}, err
	}
	if book.public.WriteVersion != "3.0" {
		return Group{}, validationError("the address book does not advertise vCard 3.0 writes")
	}
	objects, limited, err := c.scanBook(ctx, book)
	if err != nil {
		return Group{}, err
	}
	for index := range objects {
		if cardIsGroup(objects[index].card) && groupName(objects[index].card) == name {
			return Group{}, newError(CodeConflict, http.StatusConflict, "a group with this name already exists")
		}
	}
	if limited {
		return Group{}, newError(CodePartialFailure, 0, "the bounded scan could not prove that the group name is unique")
	}
	uid, err := uuidV4()
	if err != nil {
		return Group{}, err
	}
	encoded := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:-//icloud-mcp//Contacts//EN\r\n" +
		groupNameLines(name) + appleGroupField + ":group\r\nUID:" + uid + "\r\nEND:VCARD\r\n")
	if _, _, _, err := decodeRawCard(encoded); err != nil {
		return Group{}, err
	}
	if max := book.public.MaxResourceSize; max > 0 && int64(len(encoded)) > max {
		return Group{}, newError(CodePayloadTooLarge, 0, "the encoded group exceeds its resource limit")
	}
	target := childResourceURL(book.url, uid+".vcf")
	if err := c.validateURL(target); err != nil {
		return Group{}, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "text/vcard; charset=utf-8")
	header.Set("If-None-Match", "*")
	response, err := c.doDAV(ctx, http.MethodPut, target, header, encoded, maxErrorBodyBytes, book.url)
	if err != nil {
		return Group{}, err
	}
	if response.Status/100 != 2 {
		return Group{}, classifyDAVError(response, true)
	}
	result := Group{AddressBook: addressBook, UID: uid, Name: name}
	if created, getErr := c.getObject(ctx, book, target, uid); getErr == nil {
		result.ETag = readableETag(created.etag)
	}
	return result, nil
}

// RenameGroup rewrites the group's FN and N lines.
func (c *Client) RenameGroup(ctx context.Context, input *RenameGroupInput) (RenameGroupResult, error) {
	name, err := validateGroupName(input.Name)
	if err != nil {
		return RenameGroupResult{}, err
	}
	book, object, etag, err := c.writableGroup(ctx, input.AddressBook, input.UID, input.ETag)
	if err != nil {
		return RenameGroupResult{}, err
	}
	previous := groupFromObject(object, input.AddressBook)
	encoded, err := rebuildRawCard(object.raw, func(_ int, property rawProperty) ([]byte, bool) {
		switch property.name {
		case vcard.FieldFormattedName, vcard.FieldName:
			return nil, true
		}
		return nil, false
	}, []byte(groupNameLines(name)), book.public.MaxResourceSize)
	if err != nil {
		return RenameGroupResult{}, err
	}
	if err := c.putExisting(ctx, book, object, etag, encoded); err != nil {
		return RenameGroupResult{}, err
	}
	renamed := previous
	renamed.Name, renamed.ETag = name, ""
	result := RenameGroupResult{Group: renamed, PreviousName: previous.Name}
	if updated, getErr := c.getObject(ctx, book, object.url, input.UID); getErr == nil {
		result.Group = groupFromObject(updated, input.AddressBook)
	} else {
		result.ResultIncomplete = true
	}
	return result, nil
}

// DeleteGroup deletes only the group resource with If-Match. Member contacts
// are never touched.
func (c *Client) DeleteGroup(ctx context.Context, input *DeleteGroupInput) (DeleteGroupResult, error) {
	book, object, etag, err := c.writableGroup(ctx, input.AddressBook, input.UID, input.ETag)
	if err != nil {
		return DeleteGroupResult{}, err
	}
	members := groupMemberUIDs(object.raw)
	if len(members) > 0 && !input.Force {
		return DeleteGroupResult{}, validationError("the group has members; the contacts would stay, but their membership is lost. Pass force to delete it anyway")
	}
	header := make(http.Header)
	header.Set("If-Match", etag)
	response, err := c.doDAV(ctx, http.MethodDelete, object.url, header, nil, maxErrorBodyBytes, book.url)
	if err != nil {
		return DeleteGroupResult{}, err
	}
	if response.Status/100 != 2 {
		return DeleteGroupResult{}, classifyDAVError(response, false)
	}
	if members == nil {
		members = []string{}
	}
	return DeleteGroupResult{Deleted: true, AddressBook: input.AddressBook, UID: input.UID, Name: groupName(object.card), FormerMemberUIDs: members}, nil
}

func uniqueUIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > MaxGroupBatch {
		return nil, validationError("pass 1 to 100 contact UIDs")
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if err := validateUID(value); err != nil {
			return nil, err
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out, nil
}

// ChangeGroupMembers adds or removes members by editing only the group vCard.
func (c *Client) ChangeGroupMembers(ctx context.Context, input *GroupMembershipInput) (GroupMembershipResult, error) {
	uids, err := uniqueUIDs(input.ContactUIDs)
	if err != nil {
		return GroupMembershipResult{}, err
	}
	book, object, etag, err := c.writableGroup(ctx, input.AddressBook, input.UID, input.ETag)
	if err != nil {
		return GroupMembershipResult{}, err
	}
	objects, limited, err := c.scanBook(ctx, book)
	if err != nil {
		return GroupMembershipResult{}, err
	}
	contactUIDs := make(map[string]bool, len(objects))
	for index := range objects {
		if !cardIsGroup(objects[index].card) {
			contactUIDs[objects[index].card.Value(vcard.FieldUID)] = true
		}
	}
	current := make(map[string]bool)
	for _, member := range groupMemberUIDs(object.raw) {
		current[member] = true
	}
	result := GroupMembershipResult{Changed: []string{}, Unchanged: []string{}, NotFound: []string{}}
	change := make(map[string]bool)
	for _, uid := range uids {
		switch {
		case input.Add && current[uid], !input.Add && !current[uid]:
			result.Unchanged = append(result.Unchanged, uid)
		case input.Add && !contactUIDs[uid]:
			if limited {
				return GroupMembershipResult{}, newError(CodePartialFailure, 0, "the bounded scan could not prove that every contact exists")
			}
			result.NotFound = append(result.NotFound, uid)
		default:
			change[uid] = true
			result.Changed = append(result.Changed, uid)
		}
	}
	result.Group = groupFromObject(object, input.AddressBook)
	if len(change) == 0 {
		return result, nil
	}
	var extra []byte
	var replace func(int, rawProperty) ([]byte, bool)
	if input.Add {
		for _, uid := range result.Changed {
			extra = append(extra, appleMemberField+":"+appleMemberPrefix+uid+"\r\n"...)
		}
	} else {
		replace = func(_ int, property rawProperty) ([]byte, bool) {
			uid, ok := memberUID(property)
			return nil, ok && change[uid]
		}
	}
	encoded, err := rebuildRawCard(object.raw, replace, extra, book.public.MaxResourceSize)
	if err != nil {
		return GroupMembershipResult{}, err
	}
	if err := c.putExisting(ctx, book, object, etag, encoded); err != nil {
		return GroupMembershipResult{}, err
	}
	updated, getErr := c.getObject(ctx, book, object.url, input.UID)
	if getErr != nil {
		result.ResultIncomplete = true
		return result, nil
	}
	result.Group = groupFromObject(updated, input.AddressBook)
	return result, nil
}

func validateCountryCode(value string) (string, error) {
	if value == "" {
		return DefaultCountryCode, nil
	}
	code := strings.TrimPrefix(value, "+")
	if len(code) < 1 || len(code) > 3 || asciiDigits(code) != code {
		return "", validationError("country_code must be 1 to 3 digits, for example 41")
	}
	return code, nil
}

// phoneRewrite returns the rewritten raw TEL lines of a contact and the
// changes, or nil when no number changes.
func phoneRewrite(raw *rawCard, code string) (map[int][]byte, []PhoneChange) {
	lines := make(map[int][]byte)
	var changes []PhoneChange
	for index, property := range raw.properties {
		if property.name != vcard.FieldTelephone {
			continue
		}
		from := decodeTextValue(property.value)
		to, ok := InternationalPhone(from, code)
		if !ok {
			continue
		}
		lines[index] = []byte(property.header + ":" + to + "\r\n")
		changes = append(changes, PhoneChange{From: from, To: to})
	}
	return lines, changes
}

type plannedRewrite struct {
	object *addressObject
	lines  map[int][]byte
}

// NormalizePhones plans, and with Apply saves, international phone forms.
// Every save is a conditional PUT with the ETag of the scan, so a contact
// changed in the meantime fails with concurrent_modification instead of
// being overwritten. Only TEL values change; labels, parameters and every
// other property stay byte for byte.
func (c *Client) NormalizePhones(ctx context.Context, input *NormalizePhonesInput) (NormalizePhonesResult, error) {
	code, err := validateCountryCode(input.CountryCode)
	if err != nil {
		return NormalizePhonesResult{}, err
	}
	var wanted map[string]bool
	if len(input.ContactUIDs) > 0 {
		if input.GroupUID != "" {
			return NormalizePhonesResult{}, validationError("pass group_uid or contact_uids, not both")
		}
		uids, uidErr := uniqueUIDs(input.ContactUIDs)
		if uidErr != nil {
			return NormalizePhonesResult{}, uidErr
		}
		wanted = make(map[string]bool, len(uids))
		for _, uid := range uids {
			wanted[uid] = true
		}
	} else if input.GroupUID != "" {
		if err := validateUID(input.GroupUID); err != nil {
			return NormalizePhonesResult{}, err
		}
	}
	book, err := c.book(ctx, input.AddressBook)
	if err != nil {
		return NormalizePhonesResult{}, err
	}
	objects, limited, err := c.scanBook(ctx, book)
	if err != nil {
		return NormalizePhonesResult{}, err
	}
	if input.GroupUID != "" {
		found := false
		for index := range objects {
			object := &objects[index]
			if cardIsGroup(object.card) && object.card.Value(vcard.FieldUID) == input.GroupUID {
				found = true
				wanted = make(map[string]bool)
				for _, member := range groupMemberUIDs(object.raw) {
					wanted[member] = true
				}
			}
		}
		if !found {
			if limited {
				return NormalizePhonesResult{}, newError(CodePartialFailure, 0, "the bounded scan ended before the group was found")
			}
			return NormalizePhonesResult{}, newError(CodeNotFound, http.StatusNotFound, "group UID was not found in the address book")
		}
	}
	var candidates []*addressObject
	seen := make(map[string]bool)
	for index := range objects {
		object := &objects[index]
		uid := object.card.Value(vcard.FieldUID)
		if cardIsGroup(object.card) || wanted != nil && !wanted[uid] {
			continue
		}
		seen[uid] = true
		candidates = append(candidates, object)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := normalizeForSort(preferredFormattedName(candidates[i].card))
		right := normalizeForSort(preferredFormattedName(candidates[j].card))
		if left != right {
			return left < right
		}
		return candidates[i].card.Value(vcard.FieldUID) < candidates[j].card.Value(vcard.FieldUID)
	})
	result := NormalizePhonesResult{
		Applied: input.Apply, CountryCode: "+" + code, ContactsScanned: len(candidates),
		Planned: []PlannedPhoneChanges{}, ScanLimitReached: limited,
	}
	if len(input.ContactUIDs) > 0 {
		for _, uid := range input.ContactUIDs {
			if !seen[uid] && !contains(result.NotFound, uid) {
				result.NotFound = append(result.NotFound, uid)
			}
		}
	}
	var rewrites []plannedRewrite
	for _, object := range candidates {
		lines, changes := phoneRewrite(object.raw, code)
		if len(changes) == 0 {
			continue
		}
		if object.version != "3.0" {
			result.ReadOnlySkipped++
			continue
		}
		result.ContactsChanged++
		if len(result.Planned) >= MaxNormalizeChanges {
			result.Truncated = true
			continue
		}
		uid := object.card.Value(vcard.FieldUID)
		result.Planned = append(result.Planned, PlannedPhoneChanges{
			AddressBook: input.AddressBook, UID: uid, Name: truncateUTF8(preferredFormattedName(object.card), maxDisplayNameBytes), Changes: changes,
		})
		rewrites = append(rewrites, plannedRewrite{object: object, lines: lines})
	}
	if !input.Apply {
		return result, nil
	}
	saved := 0
	result.Saved = &saved
	result.Failed = []FailedContact{}
	var stop *Error
	for _, rewrite := range rewrites {
		uid := rewrite.object.card.Value(vcard.FieldUID)
		if stop != nil {
			result.Failed = append(result.Failed, FailedContact{UID: uid, Code: stop.Code, Error: stop.Message})
			continue
		}
		if err := c.savePhoneRewrite(ctx, book, rewrite); err != nil {
			typed := AsError(err)
			if typed == nil {
				typed = newError(CodeInternalError, 0, "the contact could not be saved")
			}
			result.Failed = append(result.Failed, FailedContact{UID: uid, Code: typed.Code, Error: typed.Message})
			switch typed.Code {
			case CodeRateLimited, CodeTimeout, CodeAuthentication, CodeUnavailable, CodeOutcomeUnknown:
				// Later saves would hit the same limit; report them as not tried.
				stop = &Error{Code: typed.Code, Message: "not saved: an earlier save stopped the batch; call again with this UID"}
			}
			continue
		}
		saved++
		result.SavedUIDs = append(result.SavedUIDs, uid)
	}
	return result, nil
}

func (c *Client) savePhoneRewrite(ctx context.Context, book bookRecord, rewrite plannedRewrite) error {
	etag, err := validateServerETag(rewrite.object.etag)
	if err != nil {
		return err
	}
	encoded, err := rebuildRawCard(rewrite.object.raw, func(index int, _ rawProperty) ([]byte, bool) {
		line, ok := rewrite.lines[index]
		return line, ok
	}, nil, book.public.MaxResourceSize)
	if err != nil {
		return err
	}
	return c.putExisting(ctx, book, rewrite.object, etag, encoded)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
