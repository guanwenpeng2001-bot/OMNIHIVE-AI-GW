package usagestats

import "time"

// UsageExportCursor is a keyset position inside a usage export stream. The
// stream is ordered by (created_at DESC, id DESC), so a cursor always moves
// strictly downwards: concurrent inserts cannot duplicate or skip rows that are
// already behind the cursor, which offset pagination cannot guarantee.
type UsageExportCursor struct {
	CreatedAt time.Time
	ID        int64
}
