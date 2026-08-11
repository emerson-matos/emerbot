package fiado

import "github.com/emerson/emerbot/packages/domain"

// The caderninho's three sort keys, in the user's own partition
// (PK = "USER#<id>"):
//
//	FIADO#<cliente>                    the latest
//	FIADODIA#<data>#<cliente>#<ulid>   the movement, in the day's order
//	FIADOCLI#<cliente>#<data>#<ulid>   the same movement, in the client's order
//
// They are siblings, never nested. Hanging the movements under the client
// ("FIADO#joao#…") is the obvious layout and it breaks the most frequent query:
// begins_with(SK, "FIADO#") would drag every client's whole history along just
// to list who owes, and a key condition cannot filter by suffix. With distinct
// prefixes none of them matches another — after "FIADO" comes "D", "C" or "#" —
// and every question in the caderninho costs one read.
//
// Both stores build their keys here, so the in-memory one orders and paginates
// by exactly the strings DynamoDB sorts on.
const (
	pkPrefix = "USER#"

	debtorPrefix = "FIADO#"
	dayPrefix    = "FIADODIA#"
	clientPrefix = "FIADOCLI#"
)

func partitionKey(userID string) string { return pkPrefix + userID }

func debtorSK(client string) string { return debtorPrefix + client }

// daySK orders a movement inside its day. The trailing ULID is not decoration:
// without it two movements of the same client on the same day — routine at a
// counter — would silently overwrite each other.
func daySK(date domain.CalendarDate, client, id string) string {
	return dayPrefix + date.String() + "#" + client + "#" + id
}

func dayQueryPrefix(date domain.CalendarDate) string {
	return dayPrefix + date.String() + "#"
}

func clientSK(client string, date domain.CalendarDate, id string) string {
	return clientPrefix + client + "#" + date.String() + "#" + id
}

func clientQueryPrefix(client string) string {
	return clientPrefix + client + "#"
}
