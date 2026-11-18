package fault

type Fault struct {
	Status                 int
	Reason, Message, Field string
	CurrentVersion         int64
}

func (f *Fault) Error() string { return f.Message }
func Invalid(field string) *Fault {
	return &Fault{Status: 422, Reason: "INVALID_INPUT", Message: "Invalid field value.", Field: field}
}

func Stale(v int64) *Fault {
	return &Fault{Status: 412, Reason: "VERSION_CONFLICT", Message: "The resource version has changed.", CurrentVersion: v}
}

var (
	NotFound        = &Fault{Status: 404, Reason: "NOT_FOUND", Message: "Resource not found."}
	Forbidden       = &Fault{Status: 403, Reason: "ACTION_DENIED", Message: "Action is not permitted."}
	Unauthenticated = &Fault{Status: 401, Reason: "UNAUTHENTICATED", Message: "Authentication required."}
	Conflict        = &Fault{Status: 409, Reason: "IDEMPOTENCY_CONFLICT", Message: "This idempotency key was used with different input."}
)
