package domain

// NotificationPrefs is who the daily WhatsApp messages go to. Phone mirrors the
// user's Cognito phone_number attribute — it is never entered by hand — stored
// as E.164 digits (country code + number, no leading "+"), which is what the
// Meta Cloud API's `to` field expects.
//
// UserID is the real Cognito user (sub) being notified — independent of
// shared.FinanceLedgerID, which is where the financial data being reported on
// actually lives. Several of these (one per real user) can all watch the same
// shared ledger.
//
// It used to carry a WhatsApp switch and one toggle per alert kind. They are
// gone: the pharmacy wants the messages, all of them, every day, and a switch
// nobody was ever going to flip is a branch in the notifier, a field in the
// store, a control on a page and a way for the morning to go quiet for a reason
// nobody remembers choosing. What is left is the address book — and the only
// thing that can still stop a message is not having a phone to send it to.
type NotificationPrefs struct {
	UserID string
	Phone  string
}
