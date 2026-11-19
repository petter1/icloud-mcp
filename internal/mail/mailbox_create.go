package mail

import (
	"context"
	"strings"

	"github.com/ThomasCrouzet/icloud-mcp/internal/mail/imapadapter"
)

// CreateMailbox creates one mailbox with an exact name. A name that already
// exists as a selectable mailbox returns Created=false without sending CREATE,
// so callers can use it to ensure a folder exists. Nested names require the
// parent mailbox to exist; the tool never creates intermediate levels on
// purpose.
func (s *Client) CreateMailbox(ctx context.Context, input CreateMailboxInput) (CreateMailboxResult, error) {
	if err := validateNewMailboxName(input.Name); err != nil {
		return CreateMailboxResult{}, err
	}
	const reconciliation = "List mailboxes before deciding whether to create the mailbox again."
	value, err := s.mutate(ctx, reconciliation, func(session imapSession, phase *imapMutationPhase) (any, error) {
		mailboxes, err := boundedMailboxList(session)
		if err != nil {
			return nil, err
		}
		if existing, ok := findMailbox(mailboxes, input.Name); ok {
			if !isSelectable(existing) {
				return nil, newError(CodeConflict, "a mailbox with this name exists but is not selectable")
			}
			return createMailboxResult(existing, false), nil
		}
		for _, mailbox := range mailboxes {
			if strings.EqualFold(mailbox.Name, input.Name) {
				return nil, newError(CodeConflict, "a mailbox with the same name in different letter case already exists")
			}
		}
		if len(mailboxes) >= MaxMailboxes {
			return nil, &imapadapter.Error{Kind: imapadapter.ErrorTooLarge}
		}
		if err := checkMailboxParent(mailboxes, input.Name); err != nil {
			return nil, err
		}
		phase.dispatched = true
		if err := session.CreateMailbox(input.Name); err != nil {
			return nil, mapIMAPError(err, ctx, true, reconciliation)
		}
		after, err := session.List()
		if err != nil {
			return CreateMailboxResult{Name: input.Name, Created: true, ResultIncomplete: true}, nil
		}
		created, ok := findMailbox(after, input.Name)
		if !ok {
			return CreateMailboxResult{Name: input.Name, Created: true, ResultIncomplete: true}, nil
		}
		return createMailboxResult(created, true), nil
	})
	if err != nil {
		return CreateMailboxResult{}, err
	}
	return value.(CreateMailboxResult), nil
}

func validateNewMailboxName(name string) error {
	if err := validateMailbox(name); err != nil {
		return err
	}
	if strings.TrimSpace(name) != name {
		return validationError("mailbox name cannot start or end with whitespace")
	}
	if strings.EqualFold(name, "INBOX") {
		return validationError("INBOX already exists and cannot be created")
	}
	if strings.ContainsAny(name, "*%") {
		return validationError("mailbox name cannot contain the LIST wildcards * or %")
	}
	return nil
}

func findMailbox(mailboxes []imapadapter.Mailbox, name string) (imapadapter.Mailbox, bool) {
	for _, mailbox := range mailboxes {
		if mailbox.Name == name {
			return mailbox, true
		}
	}
	return imapadapter.Mailbox{}, false
}

// checkMailboxParent rejects empty hierarchy levels and a missing parent. The
// hierarchy delimiter comes from the server's LIST response.
func checkMailboxParent(mailboxes []imapadapter.Mailbox, name string) error {
	var delimiter rune
	for _, mailbox := range mailboxes {
		if mailbox.Delimiter != 0 {
			delimiter = mailbox.Delimiter
			break
		}
	}
	if delimiter == 0 {
		return nil
	}
	sep := string(delimiter)
	if !strings.Contains(name, sep) {
		return nil
	}
	for _, part := range strings.Split(name, sep) {
		if part == "" || strings.TrimSpace(part) != part {
			return validationError("mailbox name contains an empty or padded hierarchy level")
		}
	}
	parent := name[:strings.LastIndex(name, sep)]
	if _, ok := findMailbox(mailboxes, parent); !ok {
		return newError(CodeNotFound, "parent mailbox was not returned by LIST; create it first")
	}
	return nil
}

func createMailboxResult(mailbox imapadapter.Mailbox, created bool) CreateMailboxResult {
	delimiter := ""
	if mailbox.Delimiter != 0 {
		delimiter = string(mailbox.Delimiter)
	}
	return CreateMailboxResult{
		Name:       mailbox.Name,
		Delimiter:  delimiter,
		Attributes: append([]string(nil), mailbox.Attributes...),
		Created:    created,
	}
}
